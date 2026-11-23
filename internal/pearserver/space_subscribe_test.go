package pearserver_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/authn"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const subscribePath = "/xrpc/network.habitat.space.subscribe"

var noteTp = syntax.NSID("network.habitat.note")

// subscribeFrame is a decoded subscribe stream frame.
type subscribeFrame struct {
	Type    string `json:"$type"`
	Error   string `json:"error"`
	Payload struct {
		Type   string         `json:"$type"`
		Seq    int64          `json:"seq"`
		Space  string         `json:"space"`
		Did    string         `json:"did"`
		URI    string         `json:"uri"`
		Rkey   string         `json:"rkey"`
		Cid    string         `json:"cid"`
		Record map[string]any `json:"record"`
	} `json:"payload"`
}

// setupSubscribe serves ts over HTTP and returns the subscribe URL for query.
func setupSubscribe(
	t *testing.T,
	ts *pearserver_testutil.TestServer,
	query url.Values,
) (string, func()) {
	t.Helper()
	srv := httptest.NewServer(ts.Server)
	u := "ws" + strings.TrimPrefix(srv.URL, "http") + subscribePath + "?" + query.Encode()
	return u, srv.Close
}

func readFrame(t *testing.T, conn *websocket.Conn) subscribeFrame {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	kind, raw, err := conn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, kind)
	var f subscribeFrame
	require.NoError(t, json.Unmarshal(raw, &f))
	return f
}

func TestServer_SubscribeStreamsReadableSpaces(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t, pearserver_testutil.WithValidator(
		authntest.NewSuccessValidator(&authn.CredentialInfo{
			Subject: alice,
			Method:  authn.ValidatorMethodOAuth,
			Scopes:  []string{"atproto", "space:*?authority=*&action=read"},
		}),
	))
	ctx := t.Context()
	readable, err := ts.SpaceStore.CreateSpace(ctx, org, groupTp, "readable")
	require.NoError(t, err)
	_, err = ts.PermStore.SetUserRelation(ctx, alice, readable, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)
	// Not alice's: owned elsewhere, with no role granted.
	hidden, err := ts.SpaceStore.CreateSpace(ctx, "did:plc:elsewhere", groupTp, "hidden")
	require.NoError(t, err)

	put := func(space habitat_syntax.SpaceURI, rkey string) {
		t.Helper()
		_, _, err := ts.SpaceStore.PutRecord(ctx, space, owner, noteTp, syntax.RecordKey(rkey),
			spaces_testutil.MustMarshalRecord(t, map[string]any{"text": rkey}))
		require.NoError(t, err)
	}
	put(readable, "1")
	put(hidden, "2")

	u, closeServer := setupSubscribe(t, ts, url.Values{
		"cursor":      {"0"},
		"collections": {noteTp.String()},
	})
	defer closeServer()
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, u, nil)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	defer func() { _ = resp.Body.Close() }()

	// The replay holds the readable space's record, not the hidden one's.
	f := readFrame(t, conn)
	require.Equal(t, "message", f.Type)
	require.Equal(t, "network.habitat.space.subscribe#commit", f.Payload.Type)
	require.Equal(t, readable.String(), f.Payload.Space)
	require.Equal(t, owner.String(), f.Payload.Did)
	require.Equal(t, "1", f.Payload.Rkey)
	require.Equal(t, "1", f.Payload.Record["text"])
	require.NotEmpty(t, f.Payload.Cid)
	require.Equal(t,
		habitat_syntax.ConstructSpaceRecordURI(readable, owner, noteTp, "1").String(),
		f.Payload.URI)
	replayed := f.Payload.Seq

	// Then it follows writes live, still skipping the hidden space.
	put(hidden, "3")
	put(readable, "4")
	f = readFrame(t, conn)
	require.Equal(t, "4", f.Payload.Rkey)
	require.Greater(t, f.Payload.Seq, replayed)

	require.NoError(t, ts.SpaceStore.DeleteRecord(ctx, readable, owner, noteTp, "1"))
	f = readFrame(t, conn)
	require.Equal(t, "1", f.Payload.Rkey)
	require.Nil(t, f.Payload.Record, "a delete carries no record")
}

func TestServer_SubscribeRejectsBeforeUpgrade(t *testing.T) {
	ctx := t.Context()
	space := habitat_syntax.ConstructSpaceURI(org, groupTp, "cred")
	other := habitat_syntax.ConstructSpaceURI(org, groupTp, "other")
	tests := []struct {
		name  string
		cred  *authn.CredentialInfo
		query url.Values
	}{
		{
			name: "malformed cursor",
			cred: &authn.CredentialInfo{
				Subject: alice,
				Method:  authn.ValidatorMethodOAuth,
				Scopes:  []string{"space:*?authority=*&action=read"},
			},
			query: url.Values{"cursor": {"soon"}},
		},
		{
			name: "space credential for a space the filter leaves out",
			cred: &authn.CredentialInfo{
				Space:  space,
				Method: authn.ValidatorMethodSpaceCredential,
			},
			query: url.Values{"spaces": {other.String()}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := pearserver_testutil.NewTestServer(t, pearserver_testutil.WithValidator(
				authntest.NewSuccessValidator(tt.cred),
			))
			u, closeServer := setupSubscribe(t, ts, tt.query)
			defer closeServer()
			_, resp, err := websocket.DefaultDialer.DialContext(ctx, u, nil)
			require.ErrorIs(t, err, websocket.ErrBadHandshake)
			defer func() { _ = resp.Body.Close() }()
			require.Equal(t, http.StatusBadRequest, resp.StatusCode)
			var body struct {
				Error string `json:"error"`
			}
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
			require.Equal(t, "InvalidRequest", body.Error)
		})
	}
}

// TestServer_SubscribeOrgWideByScope pins the org-wide stream an app opens
// with its org's OAuth session: every space the org owns, narrowed by the
// collections filter and by the token's space: scopes.
func TestServer_SubscribeOrgWideByScope(t *testing.T) {
	docTp := syntax.NSID("network.habitat.docs")
	otherTp := syntax.NSID("network.habitat.todo")
	tests := []struct {
		name   string
		scopes []string
		want   []string
	}{
		{
			name:   "an any-space read grant sees every org space",
			scopes: []string{"atproto", "space:*?authority=*&action=read"},
			want:   []string{"a", "b"},
		},
		{
			name:   "a type-limited grant sees only that type",
			scopes: []string{"space:network.habitat.docs?authority=*&action=read"},
			want:   []string{"a"},
		},
		{
			name:   "no space grant sees nothing",
			scopes: []string{"atproto", "transition:generic"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := pearserver_testutil.NewTestServer(t, pearserver_testutil.WithValidator(
				authntest.NewSuccessValidator(&authn.CredentialInfo{
					Subject: org,
					Method:  authn.ValidatorMethodOAuth,
					Scopes:  tt.scopes,
				}),
			))
			ctx := t.Context()
			docs, err := ts.SpaceStore.CreateSpace(ctx, org, docTp, "docs")
			require.NoError(t, err)
			todos, err := ts.SpaceStore.CreateSpace(ctx, org, otherTp, "todos")
			require.NoError(t, err)
			put := func(space habitat_syntax.SpaceURI, collection syntax.NSID, rkey string) {
				t.Helper()
				_, _, err := ts.SpaceStore.PutRecord(ctx, space, alice, collection,
					syntax.RecordKey(rkey), spaces_testutil.MustMarshalRecord(t, map[string]any{}))
				require.NoError(t, err)
			}
			put(docs, noteTp, "a")
			put(docs, "network.habitat.unwanted", "skipped")
			put(todos, noteTp, "b")
			u, closeServer := setupSubscribe(t, ts, url.Values{
				"cursor":      {"0"},
				"collections": {noteTp.String()},
			})
			defer closeServer()
			conn, resp, err := websocket.DefaultDialer.DialContext(ctx, u, nil)
			require.NoError(t, err)
			defer func() { _ = conn.Close() }()
			defer func() { _ = resp.Body.Close() }()

			var got []string
			for range tt.want {
				got = append(got, readFrame(t, conn).Payload.Rkey)
			}
			require.Equal(t, tt.want, got)
			// Nothing else follows.
			require.NoError(t, conn.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
			_, _, err = conn.ReadMessage()
			require.Error(t, err)
		})
	}
}

func TestServer_GetSnapshotPagesReadableSpaces(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t, pearserver_testutil.WithValidator(
		authntest.NewSuccessValidator(&authn.CredentialInfo{
			Subject: alice,
			Method:  authn.ValidatorMethodOAuth,
			Scopes:  []string{"atproto", "space:*?authority=*&action=read"},
		}),
	))
	ctx := t.Context()
	readable, err := ts.SpaceStore.CreateSpace(ctx, org, groupTp, "readable")
	require.NoError(t, err)
	_, err = ts.PermStore.SetUserRelation(ctx, alice, readable, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)
	hidden, err := ts.SpaceStore.CreateSpace(ctx, "did:plc:elsewhere", groupTp, "hidden")
	require.NoError(t, err)
	put := func(space habitat_syntax.SpaceURI, rkey string) {
		t.Helper()
		_, _, err := ts.SpaceStore.PutRecord(ctx, space, owner, noteTp, syntax.RecordKey(rkey),
			spaces_testutil.MustMarshalRecord(t, map[string]any{"text": rkey}))
		require.NoError(t, err)
	}
	put(readable, "1")
	put(hidden, "2")
	put(readable, "3")

	get := func(query url.Values) (int, struct {
		Events            []map[string]any `json:"events"`
		PlannedThroughSeq int64            `json:"plannedThroughSeq"`
		TipSeq            int64            `json:"tipSeq"`
	}) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet,
			"/xrpc/network.habitat.space.getSnapshot?"+query.Encode(), http.NoBody)
		rec := httptest.NewRecorder()
		ts.Server.ServeHTTP(rec, req)
		var out struct {
			Events            []map[string]any `json:"events"`
			PlannedThroughSeq int64            `json:"plannedThroughSeq"`
			TipSeq            int64            `json:"tipSeq"`
		}
		if rec.Code == http.StatusOK {
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
		}
		return rec.Code, out
	}

	code, page := get(url.Values{"limit": {"1"}, "collections": {noteTp.String()}})
	require.Equal(t, http.StatusOK, code)
	require.Len(t, page.Events, 1)
	require.Equal(t, "network.habitat.space.subscribe#commit", page.Events[0]["$type"])
	require.Equal(t, "1", page.Events[0]["rkey"])

	// The next page skips the hidden space's record and reaches the tip.
	code, next := get(url.Values{
		"limit":     {"10"},
		"afterSeq":  {strconv.FormatInt(page.PlannedThroughSeq, 10)},
		"beforeSeq": {strconv.FormatInt(page.TipSeq, 10)},
	})
	require.Equal(t, http.StatusOK, code)
	require.Len(t, next.Events, 1)
	require.Equal(t, "3", next.Events[0]["rkey"])
	require.Equal(t, page.TipSeq, next.PlannedThroughSeq)

	code, _ = get(url.Values{"limit": {"0"}})
	require.Equal(t, http.StatusBadRequest, code)
}
