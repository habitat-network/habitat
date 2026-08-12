package spaces_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/ipld/go-car"
	"github.com/stretchr/testify/require"
	"gocloud.dev/blob/memblob"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/did"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/org"
	org_testutil "github.com/habitat-network/habitat/internal/org/testutil"
	"github.com/habitat-network/habitat/internal/spacecommit"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
)

type testServerOptions struct {
	hostKey   atcrypto.PrivateKey
	validator authn.RequestValidator
	store     *spaces_testutil.TestStore
}

type testServer struct {
	*spaces.Server
	Store   *spaces_testutil.TestStore
	HostKey atcrypto.PrivateKey
}

func newTestServerWithOpts(t *testing.T, opts testServerOptions) *testServer {
	t.Helper()
	if opts.hostKey == nil {
		key, err := atcrypto.GeneratePrivateKeyK256()
		require.NoError(t, err)
		opts.hostKey = key
	}
	if opts.validator == nil {
		opts.validator = authntest.NewSuccessValidatorWithOrg(owner, orgID)
	}
	if opts.store == nil {
		opts.store = spaces_testutil.NewTestStore(t, spaces_testutil.Config{HostKey: opts.hostKey})
	}
	h, err := hive.NewHive("example.com", "pear.example.com", db_testutil.NewDB(t))
	require.NoError(t, err)
	return &testServer{
		Server: spaces.NewServer(
			opts.store,
			opts.store.FGA,
			opts.validator,
			org_testutil.NewTestStore(t),
			opts.hostKey,
			h,
			spaces.NewBlobStore(memblob.OpenBucket(nil)),
		),
		Store:   opts.store,
		HostKey: opts.hostKey,
	}
}

// newRealValidator constructs a validator that performs actual FGA
// membership checks against store (unlike the stub validators used
// elsewhere), for tests asserting that a non-member request is rejected. did
// authenticates successfully as a member of orgDID; membership in the target
// space is not implied.
func newRealValidator(store *spaces_testutil.TestStore, did, orgDID syntax.DID) authn.RequestValidator {
	return authn.NewValidator(authntest.NewSuccessMethodWithOrg(did, orgDID), nil, nil, nil, store.FGA)
}

func TestServer_CreateSpace(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{
			validator: authntest.NewSuccessValidator(
				&authn.CredentialInfo{Subject: owner, Type: authn.UserCredential},
			),
		},
	)
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.createSpace",
		strings.NewReader(`{"type": "network.habitat.group"}`),
	)
	w := httptest.NewRecorder()
	s.CreateSpace(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var output habitat.NetworkHabitatSimplespaceCreateSpaceOutput
	err := json.NewDecoder(w.Body).Decode(&output)
	require.NoError(t, err)
	require.Contains(
		t,
		output.Uri,
		"at://did:web:everyone.example.com/space/network.habitat.group/",
	)
}

func TestServer_CreateSpaceWithDidInput(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{validator: authntest.NewSuccessValidatorWithOrg(owner, orgID)},
	)

	tests := []struct {
		name    string
		did     string
		want    int
		wantErr string
	}{
		{
			name: "caller did",
			did:  owner.String(),
			want: http.StatusOK,
		},
		{
			name: "caller org",
			did:  orgID.String(),
			want: http.StatusOK,
		},
		{
			name:    "other did",
			did:     alice.String(),
			want:    http.StatusBadRequest,
			wantErr: "only caller did or caller org are allowed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(habitat.NetworkHabitatSimplespaceCreateSpaceInput{
				Did:  tt.did,
				Type: "network.habitat.group",
			})
			require.NoError(t, err)
			req := httptest.NewRequest(
				http.MethodPost,
				"/xrpc/network.habitat.simplespace.createSpace",
				bytes.NewReader(body),
			)
			w := httptest.NewRecorder()
			s.CreateSpace(w, req)

			require.Equal(t, tt.want, w.Code)
			if tt.wantErr != "" {
				var apiErr atclient.ErrorBody
				err := json.NewDecoder(w.Body).Decode(&apiErr)
				require.NoError(t, err)
				require.Equal(t, tt.wantErr, apiErr.Message)
			}
		})
	}
}

func TestServer_UploadAndGetBlob(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{},
	)
	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "blobs")
	require.NoError(t, err)

	// Upload a blob.
	upReq := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.repo.uploadBlob",
		strings.NewReader("hello blobs"),
	)
	upReq.Header.Set("Content-Type", "text/plain")
	upW := httptest.NewRecorder()
	s.UploadBlob(upW, upReq)
	require.Equal(t, http.StatusOK, upW.Code)

	var out habitat.NetworkHabitatRepoUploadBlobOutput
	require.NoError(t, json.NewDecoder(upW.Body).Decode(&out))
	require.NotEmpty(t, out.Cid)

	// Get it back through the space.
	getW := httptest.NewRecorder()
	s.GetBlob(
		getW,
		httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.space.getBlob?space="+
			url.QueryEscape(uri.String())+"&cid="+out.Cid, http.NoBody),
	)

	require.Equal(t, http.StatusOK, getW.Code)
	require.Equal(t, "text/plain", getW.Header().Get("Content-Type"))
	body, err := io.ReadAll(getW.Body)
	require.NoError(t, err)
	require.Equal(t, "hello blobs", string(body))
}

func TestServer_UploadBlob_RejectsOversized(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{},
	)

	// 500 KiB upload limit + 1 byte must be rejected.
	oversized := make([]byte, 500*1024+1)
	upReq := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.repo.uploadBlob",
		bytes.NewReader(oversized),
	)
	upReq.Header.Set("Content-Type", "application/octet-stream")
	upW := httptest.NewRecorder()
	s.UploadBlob(upW, upReq)

	require.Equal(t, http.StatusRequestEntityTooLarge, upW.Code)
}

func TestServer_ListSpaces(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{},
	)

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "my-space")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.Store.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listSpaces",
		http.NoBody,
	)
	w := httptest.NewRecorder()
	s.ListSpaces(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var output habitat.NetworkHabitatSpaceListSpacesOutput
	err = json.NewDecoder(w.Body).Decode(&output)
	require.NoError(t, err)
	require.Len(t, output.Spaces, 1)
	require.Equal(t, uri.String(), output.Spaces[0].Uri)
}

func TestServer_ListRepos(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{},
	)
	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "shared")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.Store.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listRepos?space="+uri.String(),
		http.NoBody,
	)
	w := httptest.NewRecorder()
	s.ListRepos(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var output habitat.NetworkHabitatSpaceListReposOutput
	err = json.NewDecoder(w.Body).Decode(&output)
	require.NoError(t, err)
	require.Len(t, output.Repos, 1)
	require.Equal(t, "did:plc:owner", output.Repos[0].Did)
	require.NotEmpty(t, output.Repos[0].Rev)
	// The repo's LtHash commit hash is populated (base64-encoded bytes).
	hash, ok := output.Repos[0].Hash.(string)
	require.True(t, ok)
	require.NotEmpty(t, hash)
}

// TestServer_ListRepos_Unauthorized asserts a non-member with an ordinary
// (subject-bearing) credential is rejected, exercising the validator's real
// FGA membership check rather than a stub.
func TestServer_ListRepos_Unauthorized(t *testing.T) {
	store := spaces_testutil.NewTestStore(t)
	s := newTestServerWithOpts(t, testServerOptions{
		store:     store,
		validator: newRealValidator(store, alice, orgID),
	})

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listRepos?space="+uri.String(),
		http.NoBody,
	)
	w := httptest.NewRecorder()
	s.ListRepos(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// TestServer_AddMember_Unauthorized asserts a caller without member-manager
// access to the space is rejected.
func TestServer_AddMember_Unauthorized(t *testing.T) {
	store := spaces_testutil.NewTestStore(t)
	s := newTestServerWithOpts(t, testServerOptions{
		store:     store,
		validator: newRealValidator(store, alice, orgID),
	})

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	body := `{"space": "` + uri.String() + `", "did": "did:plc:bob"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.addMember",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.AddMember(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_RemoveMember(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{},
	)

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "shared")
	require.NoError(t, err)

	err = s.Store.AddMember(t.Context(), uri, alice)
	require.NoError(t, err)

	body := `{"space": "` + uri.String() + `", "did": "did:plc:alice"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.removeMember",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.RemoveMember(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	isMember, err := s.Store.IsMember(t.Context(), orgID, uri, alice)
	require.NoError(t, err)
	require.False(t, isMember)
}

// TestServer_RemoveMember_Unauthorized asserts a caller without
// member-manager access to the space is rejected.
func TestServer_RemoveMember_Unauthorized(t *testing.T) {
	store := spaces_testutil.NewTestStore(t)
	s := newTestServerWithOpts(t, testServerOptions{
		store:     store,
		validator: newRealValidator(store, alice, orgID),
	})

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	body := `{"space": "` + uri.String() + `", "did": "did:plc:bob"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.removeMember",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.RemoveMember(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_ListMembers(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{},
	)

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "shared")
	require.NoError(t, err)

	err = s.Store.AddMember(t.Context(), uri, alice)
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.simplespace.listMembers?space="+uri.String(),
		http.NoBody,
	)
	w := httptest.NewRecorder()
	s.ListMembers(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var output habitat.NetworkHabitatSimplespaceListMembersOutput
	err = json.NewDecoder(w.Body).Decode(&output)
	require.NoError(t, err)

	var dids []string
	for _, m := range output.Members {
		dids = append(dids, m.Did)
	}
	require.ElementsMatch(t, []string{owner.String(), alice.String(), orgID.String()}, dids)
}

func TestServer_PutAndGetRecord(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{},
	)

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	body := `{"space": "` + uri.String() + `", "repo": "did:plc:owner", "collection": "network.habitat.note", "rkey": "my-note", "record": {"text": "hello"}}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.putRecord",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.PutRecord(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var putOutput habitat.NetworkHabitatSpacePutRecordOutput
	err = json.NewDecoder(w.Body).Decode(&putOutput)
	require.NoError(t, err)
	require.Contains(t, putOutput.Uri, "/network.habitat.note/my-note")

	getReq := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.getRecord?space="+uri.String()+"&collection=network.habitat.note&rkey=my-note&repo=did:plc:owner",
		http.NoBody,
	)
	getW := httptest.NewRecorder()
	s.GetRecord(getW, getReq)

	require.Equal(t, http.StatusOK, getW.Code)
	var getOutput habitat.NetworkHabitatSpaceGetRecordOutput
	err = json.NewDecoder(getW.Body).Decode(&getOutput)
	require.NoError(t, err)
	require.Equal(t, putOutput.Uri, getOutput.Uri)
	val, ok := getOutput.Value.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "hello", val["text"])
}

func TestServer_DeleteRecord(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{},
	)

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	_, _, err = s.Store.PutRecord(
		t.Context(),
		uri,
		owner,
		syntax.NSID("network.habitat.note"),
		"del-me",
		map[string]any{"x": 1},
	)
	require.NoError(t, err)

	body := `{"space": "` + uri.String() + `", "repo": "did:plc:owner", "collection": "network.habitat.note", "rkey": "del-me"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.deleteRecord",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.DeleteRecord(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	_, err = s.Store.GetRecord(
		t.Context(),
		uri,
		owner,
		syntax.NSID("network.habitat.note"),
		"del-me",
	)
	require.ErrorIs(t, err, spaces.ErrRecordNotFound)
}

// TestServer_DeleteRecordRejectsOtherRepo pins that a caller authenticated as
// one repo cannot delete another repo's records by naming it in the request
// body: the handler must reject and return before calling the store.
func TestServer_DeleteRecordRejectsOtherRepo(t *testing.T) {
	s := newTestServerWithOpts(t, testServerOptions{
		validator: authntest.NewSuccessValidatorWithOrg(alice, orgID),
	})

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	_, _, err = s.Store.PutRecord(
		t.Context(),
		uri,
		owner,
		syntax.NSID("network.habitat.note"),
		"keep-me",
		map[string]any{"x": 1},
	)
	require.NoError(t, err)

	// alice is authenticated, but names owner's repo in the request.
	body := `{"space": "` + uri.String() + `", "repo": "` + owner.String() + `", "collection": "network.habitat.note", "rkey": "keep-me"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.deleteRecord",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.DeleteRecord(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	_, err = s.Store.GetRecord(
		t.Context(),
		uri,
		owner,
		syntax.NSID("network.habitat.note"),
		"keep-me",
	)
	require.NoError(t, err, "record must survive a rejected cross-repo delete")
}

func TestServer_ListRecords(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{},
	)

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.Store.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)
	_, _, err = s.Store.PutRecord(t.Context(), uri, owner, coll, "k2", map[string]any{"x": 2})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listRecords?space="+uri.String()+"&collection=network.habitat.note&repo="+owner.String(),
		http.NoBody,
	)
	w := httptest.NewRecorder()
	s.ListRecords(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var output habitat.NetworkHabitatSpaceListRecordsOutput
	err = json.NewDecoder(w.Body).Decode(&output)
	require.NoError(t, err)
	require.Len(t, output.Records, 2)
	require.Equal(t, "k1", output.Records[0].Rkey)
	require.Equal(t, "k2", output.Records[1].Rkey)
}

// TestServer_GetRepo verifies getRepo returns a CAR whose first root is a real
// signed commit over the repo's LtHash, verifiable against the host key.
func TestServer_GetRepo(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{},
	)
	pub, err := s.HostKey.PublicKey()
	require.NoError(t, err)

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.Store.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/com.atproto.space.getRepo?space="+uri.String()+"&repo="+owner.String(),
		http.NoBody,
	)
	w := httptest.NewRecorder()
	s.GetRepo(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "application/vnd.ipld.car", w.Header().Get("Content-Type"))

	reader, err := car.NewCarReader(bytes.NewReader(w.Body.Bytes()))
	require.NoError(t, err)
	require.Len(t, reader.Header.Roots, 2)
	commitCID := reader.Header.Roots[0]

	var commitBlock []byte
	for {
		blk, err := reader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if blk.Cid() == commitCID {
			commitBlock = blk.RawData()
		}
	}
	require.NotEmpty(t, commitBlock)

	commit, err := atdata.UnmarshalCBOR(commitBlock)
	require.NoError(t, err)
	require.Equal(t, int64(spacecommit.Version), commit["ver"])

	rev, ok := commit["rev"].(string)
	require.True(t, ok)
	ikm, ok := commit["ikm"].(atdata.Bytes)
	require.True(t, ok)
	sig, ok := commit["sig"].(atdata.Bytes)
	require.True(t, ok)
	hash, ok := commit["hash"].(atdata.Bytes)
	require.True(t, ok)

	// External author (did:plc:owner) → host-signed, so verify with the host key.
	ctxBytes := spacecommit.Ctx(uri, owner, rev, ikm)
	require.NoError(t, pub.HashAndVerify(ctxBytes, sig))

	// The committed hash matches the repo's current LtHash state.
	_, wantHash, found, err := s.Store.RepoHead(t.Context(), uri, owner)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, wantHash, []byte(hash))
}

func TestServer_GetRepo_RepoNotFound(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{},
	)

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/com.atproto.space.getRepo?space="+uri.String()+"&repo="+alice.String(),
		http.NoBody,
	)
	w := httptest.NewRecorder()
	s.GetRepo(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
	require.JSONEq(t, `{"error":"RepoNotFound"}`, w.Body.String())
}

func TestServer_PutRecord_Unauthorized(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{},
	)

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	body := `{"space": "` + uri.String() + `", "repo": "did:plc:alice", "collection": "network.habitat.note", "rkey": "test", "record": {"x": 1}}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.putRecord",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.PutRecord(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_DeleteRecord_Unauthorized(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{},
	)

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	_, _, err = s.Store.PutRecord(
		t.Context(),
		uri,
		owner,
		syntax.NSID("network.habitat.note"),
		"test",
		map[string]any{"x": 1},
	)
	require.NoError(t, err)

	body := `{"space": "` + uri.String() + `", "repo": "did:plc:alice", "collection": "network.habitat.note", "rkey": "test"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.deleteRecord",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.DeleteRecord(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_Unauthorized(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{
			validator: authntest.NewFailureValidator(),
		},
	)

	body := `{"type": "network.habitat.group"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.createSpace",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.CreateSpace(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestServer_DeleteSpace(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{},
	)

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "to-delete")
	require.NoError(t, err)

	err = s.Store.AddMember(t.Context(), uri, alice)
	require.NoError(t, err)

	body := `{"space": "` + uri.String() + `"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.deleteSpace",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.DeleteSpace(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	_, err = s.Store.ListRepos(t.Context(), uri)
	require.ErrorIs(t, err, spaces.ErrSpaceNotFound)
}

// TestServer_DeleteSpace_Unauthorized asserts a caller without owner access
// to the space is rejected.
func TestServer_DeleteSpace_Unauthorized(t *testing.T) {
	store := spaces_testutil.NewTestStore(t)
	s := newTestServerWithOpts(t, testServerOptions{
		store:     store,
		validator: newRealValidator(store, alice, orgID),
	})

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	body := `{"space": "` + uri.String() + `"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.deleteSpace",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.DeleteSpace(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_ListRepoOps(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{},
	)

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")

	_, _, err = s.Store.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)
	_, _, err = s.Store.PutRecord(t.Context(), uri, owner, coll, "k2", map[string]any{"x": 2})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listRepoOps?space="+uri.String()+"&repo=did:plc:owner",
		http.NoBody,
	)
	w := httptest.NewRecorder()
	s.ListRepoOps(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var output habitat.NetworkHabitatSpaceListRepoOpsOutput
	err = json.NewDecoder(w.Body).Decode(&output)
	require.NoError(t, err)
	require.Len(t, output.Ops, 2)
	require.Equal(t, "k1", output.Ops[0].Rkey)
	require.Equal(t, "k2", output.Ops[1].Rkey)
	require.Equal(t, coll.String(), output.Ops[0].Collection)
	require.NotEmpty(t, output.Ops[0].Rev)
	require.NotEmpty(t, output.Cursor)
	require.Equal(t, output.Ops[1].Rev, output.Cursor)
}

// TestServer_ListRepoOps_Unauthorized asserts a non-member with an ordinary
// (subject-bearing) credential is rejected.
func TestServer_ListRepoOps_Unauthorized(t *testing.T) {
	store := spaces_testutil.NewTestStore(t)
	s := newTestServerWithOpts(t, testServerOptions{
		store:     store,
		validator: newRealValidator(store, alice, orgID),
	})

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listRepoOps?space="+uri.String()+"&repo=did:plc:owner",
		http.NoBody,
	)
	w := httptest.NewRecorder()
	s.ListRepoOps(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func decodeB64(t *testing.T, v any) []byte {
	t.Helper()
	str, ok := v.(string)
	require.True(t, ok, "bytes field should JSON-encode to a base64 string")
	b, err := base64.StdEncoding.DecodeString(str)
	require.NoError(t, err)
	return b
}

// TestServer_ListRepoOps_IncludesSignedCommit verifies that at the head of the
// oplog a host-signed commit is returned, and that it verifies against the host
// key with the host protocol tag and carries the repo's LtHash.
func TestServer_ListRepoOps_IncludesSignedCommit(t *testing.T) {
	hostKey, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	pub, err := hostKey.PublicKey()
	require.NoError(t, err)
	s := newTestServerWithOpts(
		t,
		testServerOptions{
			hostKey: hostKey,
		},
	)

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)
	_, _, err = s.Store.PutRecord(t.Context(), uri, owner, groupType, "k1", map[string]any{"x": 1})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listRepoOps?space="+uri.String()+"&repo="+owner.String(),
		http.NoBody,
	)
	w := httptest.NewRecorder()
	s.ListRepoOps(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var out habitat.NetworkHabitatSpaceListRepoOpsOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	require.Len(t, out.Ops, 1)
	require.Equal(t, int64(spacecommit.Version), out.Commit.Ver)
	require.Equal(t, out.Ops[0].Rev, out.Commit.Rev)

	hash := decodeB64(t, out.Commit.Hash)
	ikm := decodeB64(t, out.Commit.Ikm)
	sig := decodeB64(t, out.Commit.Sig)
	require.Len(t, ikm, 32)

	// External author (did:plc:owner) → host-signed, so verify with the host key.
	ctxBytes := spacecommit.Ctx(uri, owner, out.Commit.Rev, ikm)
	require.NoError(t, pub.HashAndVerify(ctxBytes, sig))

	_, wantHash, found, err := s.Store.RepoHead(t.Context(), uri, owner)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, wantHash, hash)
}

// TestServer_GetLatestCommit returns a host-signed commit over the repo's head
// that verifies against the host key and carries the repo's LtHash.
func TestServer_GetLatestCommit(t *testing.T) {
	hostKey, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	pub, err := hostKey.PublicKey()
	require.NoError(t, err)
	s := newTestServerWithOpts(
		t,
		testServerOptions{
			hostKey: hostKey,
		},
	)

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)
	_, _, err = s.Store.PutRecord(t.Context(), uri, owner, groupType, "k1", map[string]any{"x": 1})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.getLatestCommit?space="+uri.String()+"&repo="+owner.String(),
		http.NoBody,
	)
	w := httptest.NewRecorder()
	s.GetLatestCommit(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var out habitat.NetworkHabitatSpaceGetLatestCommitOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	require.Equal(t, int64(spacecommit.Version), out.Commit.Ver)

	hash := decodeB64(t, out.Commit.Hash)
	ikm := decodeB64(t, out.Commit.Ikm)
	sig := decodeB64(t, out.Commit.Sig)
	require.Len(t, ikm, 32)

	ctxBytes := spacecommit.Ctx(uri, owner, out.Commit.Rev, ikm)
	require.NoError(t, pub.HashAndVerify(ctxBytes, sig))

	rev, wantHash, found, err := s.Store.RepoHead(t.Context(), uri, owner)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, wantHash, hash)
	require.Equal(t, rev, out.Commit.Rev)
}

// TestServer_GetLatestCommit_EmptyRepo returns repo-not-found when the repo
// holds no records in the space.
func TestServer_GetLatestCommit_EmptyRepo(t *testing.T) {
	hostKey, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	s := newTestServerWithOpts(
		t,
		testServerOptions{
			hostKey: hostKey,
		},
	)

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.getLatestCommit?space="+uri.String()+"&repo="+owner.String(),
		http.NoBody,
	)
	w := httptest.NewRecorder()
	s.GetLatestCommit(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}

// TestServer_GetLatestCommit_Unauthorized returns space-not-found when the
// caller is not a member of the space.
func TestServer_GetLatestCommit_Unauthorized(t *testing.T) {
	store := spaces_testutil.NewTestStore(t)
	s := newTestServerWithOpts(t, testServerOptions{
		store:     store,
		validator: newRealValidator(store, alice, orgID),
	})

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)
	_, _, err = s.Store.PutRecord(t.Context(), uri, owner, groupType, "k1", map[string]any{"x": 1})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.getLatestCommit?space="+uri.String()+"&repo="+owner.String(),
		http.NoBody,
	)
	w := httptest.NewRecorder()
	s.GetLatestCommit(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_ListRepoOps_Since(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{},
	)

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")

	_, _, err = s.Store.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)
	_, _, err = s.Store.PutRecord(t.Context(), uri, owner, coll, "k2", map[string]any{"x": 2})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listRepoOps?space="+uri.String()+"&repo=did:plc:owner",
		http.NoBody,
	)
	w := httptest.NewRecorder()
	s.ListRepoOps(w, req)
	var first habitat.NetworkHabitatSpaceListRepoOpsOutput
	err = json.NewDecoder(w.Body).Decode(&first)
	require.NoError(t, err)
	require.Len(t, first.Ops, 2)

	req = httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listRepoOps?space="+uri.String()+"&repo=did:plc:owner&since="+first.Cursor,
		http.NoBody,
	)
	w = httptest.NewRecorder()
	s.ListRepoOps(w, req)
	var second habitat.NetworkHabitatSpaceListRepoOpsOutput
	err = json.NewDecoder(w.Body).Decode(&second)
	require.NoError(t, err)
	require.Len(t, second.Ops, 0)
}

func TestServer_ListRepoOps_IncludesValue(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{},
	)

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")

	_, _, err = s.Store.PutRecord(
		t.Context(),
		uri,
		owner,
		coll,
		"k1",
		map[string]any{"text": "hello"},
	)
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listRepoOps?space="+uri.String()+"&repo=did:plc:owner",
		http.NoBody,
	)
	w := httptest.NewRecorder()
	s.ListRepoOps(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var output habitat.NetworkHabitatSpaceListRepoOpsOutput
	err = json.NewDecoder(w.Body).Decode(&output)
	require.NoError(t, err)
	require.Len(t, output.Ops, 1)
	val, ok := output.Ops[0].Value.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "hello", val["text"])
}

func TestServer_ListRepoOps_ExcludeValues(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{},
	)

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")

	_, _, err = s.Store.PutRecord(
		t.Context(),
		uri,
		owner,
		coll,
		"k1",
		map[string]any{"text": "hello"},
	)
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listRepoOps?space="+uri.String()+"&repo=did:plc:owner&excludeValues=true",
		http.NoBody,
	)
	w := httptest.NewRecorder()
	s.ListRepoOps(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var output habitat.NetworkHabitatSpaceListRepoOpsOutput
	err = json.NewDecoder(w.Body).Decode(&output)
	require.NoError(t, err)
	require.Len(t, output.Ops, 1)
	require.Equal(t, "k1", output.Ops[0].Rkey)
	require.Nil(t, output.Ops[0].Value)
}

// TestServer_ListRepoOps_CommitMatchesReturnedOps asserts the response
// invariant a syncer depends on: when a signed commit is included it must
// describe exactly the state reached by folding the ops in that same response.
// A syncer folds the ops and compares its running hash against the commit, so
// a commit built from a later state than the ops reads as divergence and sends
// an otherwise healthy repo into full recovery. Writes run concurrently with
// the reads to exercise the window between listing ops and signing the head.
func TestServer_ListRepoOps_CommitMatchesReturnedOps(t *testing.T) {
	hostKey, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	s := newTestServerWithOpts(t, testServerOptions{hostKey: hostKey})

	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)
	coll := syntax.NSID("network.habitat.note")

	writesDone := make(chan struct{})
	go func() {
		defer close(writesDone)
		for i := range 60 {
			_, _, err := s.Store.PutRecord(t.Context(), uri, owner,
				coll, syntax.RecordKey(fmt.Sprintf("k%d", i)), map[string]any{"x": i})
			if err != nil {
				return
			}
		}
	}()

	for range 60 {
		req := httptest.NewRequest(
			http.MethodGet,
			"/xrpc/network.habitat.space.listRepoOps?space="+uri.String()+"&repo=did:plc:owner",
			http.NoBody,
		)
		w := httptest.NewRecorder()
		s.ListRepoOps(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		var output habitat.NetworkHabitatSpaceListRepoOpsOutput
		require.NoError(t, json.NewDecoder(w.Body).Decode(&output))
		if output.Commit.Ver == 0 {
			continue // no commit claimed for this page
		}
		want := ""
		if len(output.Ops) > 0 {
			want = output.Ops[len(output.Ops)-1].Rev
		}
		require.Equal(t, want, output.Commit.Rev,
			"commit describes a different revision than the ops returned with it")
	}
	<-writesDone
}

func TestServer_GetSpaceCredential(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{},
	)
	everyoneOrg := org.NewEveryoneOrg("everyone.example.com")
	uri, err := s.Store.CreateSpace(t.Context(), everyoneOrg.DID(), owner, groupType, "test")
	require.NoError(t, err)

	require.NoError(t, s.Store.AddMember(t.Context(), uri, alice))

	// Both tokens are minted with the host key (the space owner is external), so
	// the directory resolves the owner's atproto_space key and the delegation
	// method falls back to the host key.
	hostPub, err := s.HostKey.PublicKey()
	require.NoError(t, err)
	dir := identity.NewMockDirectory()
	dir.Insert(*did.Web("everyone.example.com").ATProtoSpaceKey(hostPub.Multibase()).Build())

	w := httptest.NewRecorder()
	s.GetDelegationToken(
		w,
		httptest.NewRequest(
			http.MethodGet,
			"/xrpc/network.habitat.space.getDelegationToken?space="+uri.String(),
			http.NoBody,
		),
	)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var delegationResp habitat.NetworkHabitatSpaceGetDelegationTokenOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&delegationResp))

	// The minted delegation token validates against the delegation auth method.
	credInfo, ok := authn.NewDelegationTokenAuthMethod(dir, s.Store.FGA, s.HostKey).Validate(
		httptest.NewRecorder(),
		authedRequest(delegationResp.Token),
	)
	require.True(t, ok)
	require.Equal(t, uri.String(), credInfo.Space.String())

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.getSpaceCredential",
		strings.NewReader(`{"space": "`+uri.String()+`"}`),
	)
	req.Header.Set("Authorization", "Bearer "+delegationResp.Token)
	w = httptest.NewRecorder()
	s.GetSpaceCredential(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var spaceCredResp habitat.NetworkHabitatSpaceGetSpaceCredentialOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&spaceCredResp))

	// The minted space credential validates against the space credential auth
	// method, resolving the owner's atproto_space key from the directory.
	credInfo, ok = authn.NewSpaceCredentialAuthMethod(dir).Validate(
		httptest.NewRecorder(),
		authedRequest(spaceCredResp.Credential),
	)
	require.True(t, ok)
	require.Equal(t, uri.String(), credInfo.Space.String())

	var claims jwt.MapClaims
	_, err = jwt.ParseWithClaims(
		spaceCredResp.Credential,
		&claims,
		func(t *jwt.Token) (interface{}, error) {
			return s.HostKey.PublicKey()
		},
	)
	require.NoError(t, err)
	require.Equal(t, uri.String(), claims["sub"])
}

// authedRequest returns a request carrying the given bearer token, for feeding
// a minted token back into its validating auth method.
func authedRequest(token string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

// TestServer_ListRepoOpsSinceAheadRejects pins that a since beyond the repo
// head is an error (not an empty page), so an ahead-of-host syncer falls back
// to a full recovery instead of silently stopping.
func TestServer_ListRepoOpsSinceAheadRejects(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{},
	)
	uri, err := s.Store.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.Store.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"v": 1})
	require.NoError(t, err)

	// A TID-like string that sorts after any real TID (base32 is a-z + 2-7).
	ahead := strings.Repeat("z", 13)
	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listRepoOps?space="+uri.String()+
			"&repo="+owner.String()+"&since="+ahead,
		http.NoBody,
	)
	w := httptest.NewRecorder()
	s.ListRepoOps(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	var body atclient.ErrorBody
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	require.Equal(t, "RevNotFound", body.Name)
}
