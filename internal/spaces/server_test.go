package spaces_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipld/go-car"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/hive"
	org_testutil "github.com/habitat-network/habitat/internal/org/testutil"
	"github.com/habitat-network/habitat/internal/spacecommit"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
)

func newTestServer(t *testing.T, oauth, serviceAuth authn.Method) (*spaces.Server, spaces.Store) {
	return newTestServerWithSigners(t, oauth, serviceAuth, nil)
}

func newTestServerWithSigners(
	t *testing.T,
	oauth, serviceAuth authn.Method,
	host atcrypto.PrivateKey,
) (*spaces.Server, spaces.Store) {
	t.Helper()
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })
	sp := spaces_testutil.NewTestStore(t, spaces_testutil.Config{FgaStore: fga})
	h := hive.NewHive("example.com", "pear.example.com", db_testutil.NewDB(t))
	return spaces.NewServer(
		sp,
		fga,
		oauth,
		serviceAuth,
		authn.NewDelegationTokenAuthMethod(nil, nil),
		org_testutil.NewTestStore(t),
		host,
		h,
	), sp
}

func newOwnerServer(t *testing.T) (*spaces.Server, spaces.Store) {
	return newTestServer(t,
		authntest.NewSuccessMethodWithOrg(owner, orgId),
		authntest.NewSuccessMethodWithOrg(owner, orgId),
	)
}

func newAliceServer(t *testing.T) (*spaces.Server, spaces.Store) {
	return newTestServer(t,
		authntest.NewSuccessMethodWithOrg(alice, orgId),
		authntest.NewSuccessMethodWithOrg(alice, orgId),
	)
}

func TestServer_CreateSpace(t *testing.T) {
	s, _ := newOwnerServer(t)

	body := `{"type": "network.habitat.group"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.createSpace",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.CreateSpace(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var output habitat.NetworkHabitatSpaceCreateSpaceOutput
	err := json.NewDecoder(w.Body).Decode(&output)
	require.NoError(t, err)
	require.Contains(t, output.Uri, "ats://did:web:public.habitat.network/network.habitat.group/")
}

func TestServer_ListSpaces(t *testing.T) {
	s, store := newOwnerServer(t)

	uri, err := store.CreateSpace(t.Context(), orgId, owner, groupType, "my-space")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.space.listSpaces", nil)
	w := httptest.NewRecorder()
	s.ListSpaces(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var output habitat.NetworkHabitatSpaceListSpacesOutput
	err = json.NewDecoder(w.Body).Decode(&output)
	require.NoError(t, err)
	require.Len(t, output.Spaces, 1)
	require.Equal(t, uri.String(), output.Spaces[0].Uri)
	require.Equal(t, output.Spaces[0].MemberCount, int64(2))
}

func TestServer_ListRepos(t *testing.T) {
	s, store := newOwnerServer(t)

	uri, err := store.CreateSpace(t.Context(), orgId, owner, groupType, "shared")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = store.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listRepos?space="+uri.String(),
		nil,
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

func TestServer_ListRepos_CursorLimitNotSupported(t *testing.T) {
	s, store := newOwnerServer(t)

	uri, err := store.CreateSpace(t.Context(), orgId, owner, groupType, "shared")
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listRepos?space="+uri.String()+"&cursor=abc",
		nil,
	)
	w := httptest.NewRecorder()
	s.ListRepos(w, req)
	require.Equal(t, http.StatusNotImplemented, w.Code)

	req = httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listRepos?space="+uri.String()+"&limit=10",
		nil,
	)
	w = httptest.NewRecorder()
	s.ListRepos(w, req)
	require.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestServer_ListRepos_Unauthorized(t *testing.T) {
	s, store := newAliceServer(t)

	uri, err := store.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listRepos?space="+uri.String(),
		nil,
	)
	w := httptest.NewRecorder()
	s.ListRepos(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_RemoveMember(t *testing.T) {
	s, store := newOwnerServer(t)

	uri, err := store.CreateSpace(t.Context(), orgId, owner, groupType, "shared")
	require.NoError(t, err)

	err = store.AddMember(t.Context(), uri, alice, spaces.SpaceAccessRead)
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

	isMember, err := store.IsMember(t.Context(), orgId, uri, alice)
	require.NoError(t, err)
	require.False(t, isMember)
}

func TestServer_PutAndGetRecord(t *testing.T) {
	s, store := newOwnerServer(t)

	uri, err := store.CreateSpace(t.Context(), orgId, owner, groupType, "test")
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
		nil,
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
	s, store := newOwnerServer(t)

	uri, err := store.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	_, _, err = store.PutRecord(
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

	_, err = store.GetRecord(
		t.Context(),
		uri,
		owner,
		syntax.NSID("network.habitat.note"),
		"del-me",
	)
	require.ErrorIs(t, err, spaces.ErrRecordNotFound)
}

func TestServer_ListRecords(t *testing.T) {
	s, store := newOwnerServer(t)

	uri, err := store.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = store.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)
	_, _, err = store.PutRecord(t.Context(), uri, owner, coll, "k2", map[string]any{"x": 2})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listRecords?space="+uri.String()+"&collection=network.habitat.note&repo="+owner.String(),
		nil,
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
	hostKey, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	pub, err := hostKey.PublicKey()
	require.NoError(t, err)
	m := authntest.NewSuccessMethodWithOrg(owner, orgId)
	s, store := newTestServerWithSigners(t, m, m, hostKey)

	uri, err := store.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = store.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/com.atproto.space.getRepo?space="+uri.String()+"&repo="+owner.String(),
		nil,
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

	// External author (did:plc:owner) → host-signed under the host tag.
	ctxBytes := spacecommit.Ctx(spacecommit.HostProtocolTag, uri, owner, rev, ikm)
	require.NoError(t, pub.HashAndVerify(ctxBytes, sig))

	// The committed hash matches the repo's current LtHash state.
	_, wantHash, found, err := store.RepoHead(t.Context(), uri, owner)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, wantHash, []byte(hash))
}

func TestServer_GetRepo_RepoNotFound(t *testing.T) {
	s, store := newOwnerServer(t)

	uri, err := store.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/com.atproto.space.getRepo?space="+uri.String()+"&repo="+alice.String(),
		nil,
	)
	w := httptest.NewRecorder()
	s.GetRepo(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
	require.JSONEq(t, `{"error":"RepoNotFound"}`, w.Body.String())
}

func TestServer_AddMember_Unauthorized(t *testing.T) {
	s, store := newAliceServer(t)

	uri, err := store.CreateSpace(t.Context(), orgId, owner, groupType, "test")
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

func TestServer_RemoveMember_Unauthorized(t *testing.T) {
	s, store := newAliceServer(t)

	uri, err := store.CreateSpace(t.Context(), orgId, owner, groupType, "test")
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

func TestServer_PutRecord_Unauthorized(t *testing.T) {
	s, store := newAliceServer(t)

	uri, err := store.CreateSpace(t.Context(), orgId, owner, groupType, "test")
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
	s, store := newAliceServer(t)

	uri, err := store.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	_, _, err = store.PutRecord(
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
	s, _ := newTestServer(t,
		authntest.NewFailMethod(),
		authntest.NewFailMethod(),
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
	s, store := newOwnerServer(t)

	uri, err := store.CreateSpace(t.Context(), orgId, owner, groupType, "to-delete")
	require.NoError(t, err)

	err = store.AddMember(t.Context(), uri, alice, spaces.SpaceAccessRead)
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

	_, err = store.ListRepos(t.Context(), uri)
	require.ErrorIs(t, err, spaces.ErrSpaceNotFound)
}

func TestServer_DeleteSpace_Unauthorized(t *testing.T) {
	s, store := newAliceServer(t)

	uri, err := store.CreateSpace(t.Context(), orgId, owner, groupType, "test")
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
	s, store := newOwnerServer(t)

	uri, err := store.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")

	_, _, err = store.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)
	_, _, err = store.PutRecord(t.Context(), uri, owner, coll, "k2", map[string]any{"x": 2})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listRepoOps?space="+uri.String()+"&repo=did:plc:owner",
		nil,
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
	m := authntest.NewSuccessMethodWithOrg(owner, orgId)
	s, store := newTestServerWithSigners(t, m, m, hostKey)

	uri, err := store.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)
	_, _, err = store.PutRecord(t.Context(), uri, owner, groupType, "k1", map[string]any{"x": 1})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listRepoOps?space="+uri.String()+"&repo="+owner.String(),
		nil,
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

	// External author (did:plc:owner) → host-signed under the host tag.
	ctxBytes := spacecommit.Ctx(spacecommit.HostProtocolTag, uri, owner, out.Commit.Rev, ikm)
	require.NoError(t, pub.HashAndVerify(ctxBytes, sig))

	_, wantHash, found, err := store.RepoHead(t.Context(), uri, owner)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, wantHash, hash)
}

// TestServer_ListRepoOps_NoCommitWithoutSigner confirms the commit is omitted
// when no signer can cover the repo owner.
func TestServer_ListRepoOps_NoCommitWithoutSigner(t *testing.T) {
	s, store := newOwnerServer(t) // no host key configured

	uri, err := store.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)
	_, _, err = store.PutRecord(t.Context(), uri, owner, groupType, "k1", map[string]any{"x": 1})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listRepoOps?space="+uri.String()+"&repo="+owner.String(),
		nil,
	)
	w := httptest.NewRecorder()
	s.ListRepoOps(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var out habitat.NetworkHabitatSpaceListRepoOpsOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	require.Len(t, out.Ops, 1)
	require.Zero(t, out.Commit.Ver, "commit omitted when no signer is available")
}

func TestServer_ListRepoOps_Since(t *testing.T) {
	s, store := newOwnerServer(t)

	uri, err := store.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")

	_, _, err = store.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)
	_, _, err = store.PutRecord(t.Context(), uri, owner, coll, "k2", map[string]any{"x": 2})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listRepoOps?space="+uri.String()+"&repo=did:plc:owner",
		nil,
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
		nil,
	)
	w = httptest.NewRecorder()
	s.ListRepoOps(w, req)
	var second habitat.NetworkHabitatSpaceListRepoOpsOutput
	err = json.NewDecoder(w.Body).Decode(&second)
	require.NoError(t, err)
	require.Len(t, second.Ops, 0)
}

func TestServer_ListRepoOps_Unauthorized(t *testing.T) {
	s, store := newAliceServer(t)

	uri, err := store.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listRepoOps?space="+uri.String()+"&repo=did:plc:owner",
		nil,
	)
	w := httptest.NewRecorder()
	s.ListRepoOps(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_ListRepoOps_IncludesValue(t *testing.T) {
	s, store := newOwnerServer(t)

	uri, err := store.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")

	_, _, err = store.PutRecord(
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
		nil,
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
	s, store := newOwnerServer(t)

	uri, err := store.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")

	_, _, err = store.PutRecord(
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
		nil,
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
