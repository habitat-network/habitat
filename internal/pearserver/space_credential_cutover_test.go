package pearserver_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/httpsig"
	pear_test "github.com/habitat-network/habitat/internal/pearserver/testutil"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/spacecommit"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_test "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
)

type cutoverSearch struct {
	search.Index
	queries int
}

func (s *cutoverSearch) Search(context.Context, search.Query) (search.Result, error) {
	s.queries++
	return search.Result{}, nil
}

type cutoverCredential struct {
	token string
	key   *atcrypto.PrivateKeyP256
}

func bindCutoverCredential(
	t *testing.T, key atcrypto.PrivateKey, space habitat_syntax.SpaceURI,
) cutoverCredential {
	t.Helper()
	binding, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	pub, err := binding.PublicKey()
	require.NoError(t, err)
	token, err := utils.SpaceCredential(key, space, utils.WithConfirmationKey(pub.DIDKey()))
	require.NoError(t, err)
	return cutoverCredential{token: token, key: binding}
}

func (c cutoverCredential) sign(t *testing.T, req *http.Request, aud syntax.DID) {
	t.Helper()
	req.Header.Set("Authorization", httpsig.CredentialScheme+" "+c.token)
	req.Header.Set(httpsig.AudienceHeader, aud.String())
	require.NoError(t, httpsig.Sign(req, c.key, "authorization", "atproto-space-audience"))
}

func cutoverSpace(
	t *testing.T, ts *pear_test.TestServer, handle string, nsid syntax.NSID,
) (habitat_syntax.SpaceURI, cutoverCredential) {
	t.Helper()
	ctx := t.Context()
	id, err := ts.Hive.MintIdentity(ctx, handle, "example.com")
	require.NoError(t, err)
	uri, err := ts.SpaceStore.CreateSpace(ctx, id.DID, nsid, "self")
	require.NoError(t, err)
	key, err := ts.Hive.PrivateKeyForDID(ctx, id.DID)
	require.NoError(t, err)
	return uri, bindCutoverCredential(t, key, uri)
}

func markCutover(t *testing.T, ts *pear_test.TestServer, space habitat_syntax.SpaceURI) {
	t.Helper()
	require.NoError(t, ts.DB.Exec(
		"INSERT INTO canonical_group_rows (did) VALUES (?)", space.SpaceOwner(),
	).Error)
}

func putCutoverNote(
	t *testing.T,
	ts *pear_test.TestServer,
	space habitat_syntax.SpaceURI,
	key string,
) {
	t.Helper()
	_, _, err := ts.SpaceStore.PutRecord(t.Context(), space, owner, noteTp, syntax.RecordKey(key),
		spaces_test.MustMarshalRecord(t, map[string]any{"text": key}))
	require.NoError(t, err)
}

func TestLegacyCredentialUseAcrossCutover(t *testing.T) {
	for _, mode := range []string{"marker", "staged policy"} {
		t.Run(mode, func(t *testing.T) {
			idx := &cutoverSearch{}
			ts := pear_test.NewTestServer(t, pear_test.WithVerifiedSpaceCredentials(),
				pear_test.WithSearchIndex(idx))
			ctx := t.Context()
			space, cred := cutoverSpace(t, ts, "cutover", groupTp)
			const record = "cutover-note"
			putCutoverNote(t, ts, space, record)
			const blob = "cutover blob"
			c, _, err := ts.BlobStore.PutBlob(ctx, "text/plain", []byte(blob))
			require.NoError(t, err)
			_, _, err = ts.SpaceStore.PutRecord(ctx, space, owner, noteTp, "attachment",
				spaces_test.MustMarshalRecord(t, map[string]any{"attachment": map[string]any{
					"$type": "blob", "ref": map[string]any{"$link": c.String()},
					"mimeType": "text/plain", "size": len(blob),
				}}))
			require.NoError(t, err)
			mac, _, _ := encapsulate(t, ts.HostKEMKey, spacecommit.RecordMACType)

			type probe struct {
				method, path, body, want, mac string
				audience                      syntax.DID
			}
			scope := url.Values{"space": {space.AtSpace().String()}}
			repo := url.Values{"space": scope["space"], "repo": {owner.String()}}
			get := url.Values{
				"space": scope["space"], "repo": repo["repo"],
				"collection": {noteTp.String()}, "rkey": {record},
			}
			blobQuery := url.Values{"space": scope["space"], "cid": {c.String()}}
			probes := []probe{
				{
					path:     "network.habitat.space.listRecords?" + repo.Encode(),
					want:     record,
					audience: owner,
				},
				{
					path:     "com.atproto.space.getRecord?" + get.Encode(),
					want:     record,
					audience: owner,
					mac:      mac,
				},
				{path: "com.atproto.space.getBlob?" + blobQuery.Encode(), want: blob},
				{path: "network.habitat.space.listRepos?" + scope.Encode(), want: owner.String()},
				{path: "com.atproto.space.listWriters?" + scope.Encode(), want: owner.String()},
				{
					path: "network.habitat.relationship.listRelations?" + scope.Encode(),
					want: `"relations"`,
				},
				{path: "network.habitat.space.searchRecords?q=note", want: `"records"`},
				{path: "network.habitat.space.getSnapshot", want: record},
				{
					method: http.MethodPost,
					path:   "network.habitat.space.registerNotify",
					body: `{"space":"` + space.AtSpace().
						String() +
						`","endpoint":"https://sync.example/first"}`,
					want: `"expiresAt"`,
				},
			}
			run := func(p probe) *httptest.ResponseRecorder {
				t.Helper()
				req := httptest.NewRequest(p.method, "/xrpc/"+p.path, strings.NewReader(p.body))
				aud := p.audience
				if aud == "" {
					aud = space.SpaceOwner()
				}
				cred.sign(t, req, aud)
				if p.mac != "" {
					req.Header.Set(spacecommit.AcceptSignatureHeader, p.mac)
				}
				w := httptest.NewRecorder()
				ts.Server.ServeHTTP(w, req)
				return w
			}
			for _, p := range probes {
				w := run(p)
				require.Equal(t, http.StatusOK, w.Code, "%s: %s", p.path, w.Body.String())
				require.Contains(t, w.Body.String(), p.want, p.path)
			}
			require.Equal(t, 1, idx.queries)
			if mode == "marker" {
				markCutover(t, ts, space)
			} else {
				require.NoError(t, spaces.SetGroupSpacePolicy(ctx, ts.SpaceStore, space,
					spaces.Policy{Type: spaces.PolicyAtgrp}))
			}
			for _, p := range probes {
				p.body = strings.ReplaceAll(p.body, "/first", "/second")
				w := run(p)
				require.Equal(t, http.StatusUnauthorized, w.Code, "%s: %s", p.path, w.Body.String())
				require.JSONEq(
					t,
					`{"error":"Unauthorized","message":"space credential is not accepted"}`,
					w.Body.String(),
					p.path,
				)
			}
			require.Equal(t, 1, idx.queries)
			regs, err := ts.NotifyStore.ListForRepo(ctx, space, owner)
			require.NoError(t, err)
			require.Len(t, regs, 1, "existing registrations are a separate drain gate")
			require.Equal(t, "https://sync.example/first", regs[0].Endpoint)
		})
	}
}

func TestLegacyCredentialStreamsStopAtCutover(t *testing.T) {
	ts := pear_test.NewTestServer(t, pear_test.WithVerifiedSpaceCredentials())
	space, cred := cutoverSpace(t, ts, "cutover", groupTp)
	legacy, legacyCred := cutoverSpace(t, ts, "legacy", groupTp)
	putCutoverNote(t, ts, space, "before")
	putCutoverNote(t, ts, legacy, "legacy-before")
	target, closeServer := setupSubscribe(t, ts, url.Values{"cursor": {"0"}})
	t.Cleanup(closeServer)
	dial := func(uri habitat_syntax.SpaceURI, credential cutoverCredential) (*websocket.Conn, *http.Response, error) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, target, http.NoBody)
		credential.sign(t, req, uri.SpaceOwner())
		return websocket.DefaultDialer.DialContext(t.Context(), target, req.Header)
	}
	conn, response, err := dial(space, cred)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	t.Cleanup(func() { _ = conn.Close() })
	unchanged, legacyResponse, err := dial(legacy, legacyCred)
	require.NoError(t, err)
	require.NoError(t, legacyResponse.Body.Close())
	t.Cleanup(func() { _ = unchanged.Close() })
	require.Equal(t, "before", readFrame(t, conn).Payload.Rkey)
	require.Equal(t, "legacy-before", readFrame(t, unchanged).Payload.Rkey)

	markCutover(t, ts, space)
	putCutoverNote(t, ts, space, "after")
	putCutoverNote(t, ts, legacy, "legacy-after")
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(4*time.Second)))
	_, _, err = conn.ReadMessage()
	var closeErr *websocket.CloseError
	require.ErrorAs(t, err, &closeErr, "converted stream must close, not time out or emit a record")
	require.Equal(t, "legacy-after", readFrame(t, unchanged).Payload.Rkey)
	_, response, err = dial(space, cred)
	require.ErrorIs(t, err, websocket.ErrBadHandshake)
	defer func() { _ = response.Body.Close() }()
	require.Equal(t, http.StatusUnauthorized, response.StatusCode)
}

func TestLegacyCredentialRemoteAndInboxControls(t *testing.T) {
	key, err := atcrypto.GeneratePrivateKeyK256()
	require.NoError(t, err)
	pub, err := key.PublicKey()
	require.NoError(t, err)
	remote := syntax.DID("did:web:remote.example")
	dir := identity.NewMockDirectory()
	dir.Insert(identity.Identity{
		DID: remote,
		Keys: map[string]identity.VerificationMethod{
			"atproto": {Type: "Multikey", PublicKeyMultibase: pub.Multibase()},
		},
	})
	ts := pear_test.NewTestServer(
		t,
		pear_test.WithVerifiedSpaceCredentials(),
		pear_test.WithDirectory(dir),
	)
	inbox, person := cutoverSpace(t, ts, "person", "org.atgrp.inbox")
	absent := habitat_syntax.ConstructSpaceURI(remote, groupTp, "self")
	remoteCred := bindCutoverCredential(t, key, absent)
	call := func(space habitat_syntax.SpaceURI, cred cutoverCredential) *httptest.ResponseRecorder {
		t.Helper()
		query := url.Values{"space": {space.String()}, "repo": {owner.String()}}
		req := httptest.NewRequest(
			http.MethodGet,
			"/xrpc/com.atproto.space.listRecords?"+query.Encode(),
			http.NoBody,
		)
		cred.sign(t, req, owner)
		w := httptest.NewRecorder()
		ts.Server.ServeHTTP(w, req)
		return w
	}
	putCutoverNote(t, ts, inbox, "inbox-note")
	w := call(inbox, person)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "inbox-note")
	w = call(absent, remoteCred)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.JSONEq(t, `{"records":[]}`, w.Body.String())

	// Force lookup errors on the disposable test DB.
	require.NoError(t, ts.DB.Exec("DROP TABLE spaces").Error)
	w = call(inbox, person)
	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "inbox-note")
	require.NoError(t, ts.DB.Exec("DROP TABLE canonical_group_rows").Error)
	w = call(absent, remoteCred)
	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), `"records"`)
}
