package spaces_server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipld/go-car"
	"github.com/stretchr/testify/require"
	"gocloud.dev/blob/memblob"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/spacecommit"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_server "github.com/habitat-network/habitat/internal/spaces/server"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

type opts struct {
	validator authn.RequestValidator
}

type Option func(*opts)

func WithValidator(validator authn.RequestValidator) Option {
	return func(o *opts) {
		o.validator = validator
	}
}

var (
	orgID     = syntax.DID("did:plc:org")
	owner     = syntax.DID("did:plc:owner")
	alice     = syntax.DID("did:plc:alice")
	groupType = syntax.NSID("network.habitat.group")
)

func newTestStore(t *testing.T) (atcrypto.PrivateKey, spaces.Store) {
	t.Helper()

	key, err := atcrypto.GeneratePrivateKeyK256()
	require.NoError(t, err)
	return key, spaces_testutil.NewTestStore(t, spaces_testutil.WithHostKey(key))
}

// mustMarshalRecord validates and CBOR-encodes value the way a real
// PutRecord caller must before calling the store's PutRecord.
func mustMarshalRecord(t *testing.T, value any) []byte {
	t.Helper()
	bytes, err := spaces.MarshalRecord(value)
	require.NoError(t, err)
	return bytes
}

func newTestServerWithOpts(
	t *testing.T,
	key atcrypto.PrivateKey,
	store spaces.Store,
	options ...Option,
) *spaces_server.Server {
	t.Helper()

	o := &opts{
		validator: authntest.NewSuccessValidatorWithOrg(owner, orgID),
	}

	for _, option := range options {
		option(o)
	}

	h, err := hive.NewHive("example.com", "pear.example.com", db_testutil.NewDB(t))
	require.NoError(t, err)
	return spaces_server.NewServer(
		store,
		o.validator,
		key,
		h,
		spaces.NewBlobStore(memblob.OpenBucket(nil)),
	)
}

func TestServer_UploadAndGetBlob(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(
		t,
		key,
		store,
	)

	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "blobs")
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
	key, store := newTestStore(t)
	s := newTestServerWithOpts(
		t,
		key,
		store,
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
	key, store := newTestStore(t)
	s := newTestServerWithOpts(
		t,
		key,
		store,
	)

	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "my-space")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = store.PutRecord(t.Context(), uri, owner, coll, "k1", mustMarshalRecord(t, map[string]any{"x": 1}))
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
	key, store := newTestStore(t)
	s := newTestServerWithOpts(
		t,
		key,
		store,
	)

	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "shared")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = store.PutRecord(t.Context(), uri, owner, coll, "k1", mustMarshalRecord(t, map[string]any{"x": 1}))
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
	// The repo's LtHash commit hash is populated.
	require.NotEmpty(t, output.Repos[0].Hash)
}

func TestServer_PutAndGetRecord(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(
		t,
		key,
		store,
	)
	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
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
	key, store := newTestStore(t)
	s := newTestServerWithOpts(
		t,
		key,
		store,
	)
	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	_, _, err = store.PutRecord(
		t.Context(),
		uri,
		owner,
		syntax.NSID("network.habitat.note"),
		"del-me",
		mustMarshalRecord(t, map[string]any{"x": 1}),
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
	key, store := newTestStore(t)
	s := newTestServerWithOpts(
		t,
		key,
		store,
	)

	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = store.PutRecord(t.Context(), uri, owner, coll, "k1", mustMarshalRecord(t, map[string]any{"x": 1}))
	require.NoError(t, err)
	_, _, err = store.PutRecord(t.Context(), uri, owner, coll, "k2", mustMarshalRecord(t, map[string]any{"x": 2}))
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
	key, store := newTestStore(t)
	s := newTestServerWithOpts(
		t,
		key,
		store,
	)

	pub, err := key.PublicKey()
	require.NoError(t, err)

	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = store.PutRecord(t.Context(), uri, owner, coll, "k1", mustMarshalRecord(t, map[string]any{"x": 1}))
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
	_, wantHash, found, err := store.RepoHead(t.Context(), uri, owner)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, wantHash, []byte(hash))
}

func TestServer_GetRepo_RepoNotFound(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(
		t,
		key,
		store,
	)

	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
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

// TestServer_GetRepo_SpaceNotFound distinguishes an unknown space (400,
// SpaceNotFound) from a known space with no repo (404, RepoNotFound, above).
func TestServer_GetRepo_SpaceNotFound(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(
		t,
		key,
		store,
	)
	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")
	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/com.atproto.space.getRepo?space="+uri.String()+"&repo="+owner.String(),
		http.NoBody,
	)
	w := httptest.NewRecorder()
	s.GetRepo(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.JSONEq(t, `{"error":"SpaceNotFound"}`, w.Body.String())
}

func TestServer_PutRecord_Unauthorized(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(
		t,
		key,
		store,
	)
	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
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

// TestServer_PutRecord_InvalidRecord pins that a record violating the
// atproto data model (e.g. a non-integer float, which atproto only
// represents as integers) surfaces as a 400 InvalidRequest, not a 500.
func TestServer_PutRecord_InvalidRecord(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(
		t,
		key,
		store,
	)
	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	body := `{"space": "` + uri.String() + `", "repo": "did:plc:owner", "collection": "network.habitat.note", "rkey": "my-note", "record": {"x": 0.15}}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.putRecord",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.PutRecord(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)

	_, err = store.GetRecord(
		t.Context(),
		uri,
		owner,
		syntax.NSID("network.habitat.note"),
		"my-note",
	)
	require.ErrorIs(t, err, spaces.ErrRecordNotFound)
}

func TestServer_DeleteRecord_Unauthorized(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(
		t,
		key,
		store,
	)

	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	_, _, err = store.PutRecord(
		t.Context(),
		uri,
		owner,
		syntax.NSID("network.habitat.note"),
		"test",
		mustMarshalRecord(t, map[string]any{"x": 1}),
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

func TestServer_ListRepoOps(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(
		t,
		key,
		store,
	)

	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")

	_, _, err = store.PutRecord(t.Context(), uri, owner, coll, "k1", mustMarshalRecord(t, map[string]any{"x": 1}))
	require.NoError(t, err)
	_, _, err = store.PutRecord(t.Context(), uri, owner, coll, "k2", mustMarshalRecord(t, map[string]any{"x": 2}))
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

// TestServer_ListRepoOps_IncludesSignedCommit verifies that at the head of the
// oplog a host-signed commit is returned, and that it verifies against the host
// key with the host protocol tag and carries the repo's LtHash.
func TestServer_ListRepoOps_IncludesSignedCommit(t *testing.T) {
	key, store := newTestStore(t)
	pub, err := key.PublicKey()
	require.NoError(t, err)

	s := newTestServerWithOpts(
		t,
		key,
		store,
	)

	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)
	_, _, err = store.PutRecord(t.Context(), uri, owner, groupType, "k1", mustMarshalRecord(t, map[string]any{"x": 1}))
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

	hash := []byte(out.Commit.Hash)
	ikm := []byte(out.Commit.Ikm)
	sig := []byte(out.Commit.Sig)
	require.Len(t, ikm, 32)

	// External author (did:plc:owner) → host-signed, so verify with the host key.
	ctxBytes := spacecommit.Ctx(uri, owner, out.Commit.Rev, ikm)
	require.NoError(t, pub.HashAndVerify(ctxBytes, sig))

	_, wantHash, found, err := store.RepoHead(t.Context(), uri, owner)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, wantHash, hash)
}

// TestServer_GetLatestCommit returns a host-signed commit over the repo's head
// that verifies against the host key and carries the repo's LtHash.
func TestServer_GetLatestCommit(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(
		t,
		key,
		store,
	)

	pub, err := key.PublicKey()
	require.NoError(t, err)

	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)
	_, _, err = store.PutRecord(t.Context(), uri, owner, groupType, "k1", mustMarshalRecord(t, map[string]any{"x": 1}))
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

	hash := []byte(out.Commit.Hash)
	ikm := []byte(out.Commit.Ikm)
	sig := []byte(out.Commit.Sig)
	require.Len(t, ikm, 32)

	ctxBytes := spacecommit.Ctx(uri, owner, out.Commit.Rev, ikm)
	require.NoError(t, pub.HashAndVerify(ctxBytes, sig))

	rev, wantHash, found, err := store.RepoHead(t.Context(), uri, owner)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, wantHash, hash)
	require.Equal(t, rev, out.Commit.Rev)
}

// TestServer_GetLatestCommit_EmptyRepo returns repo-not-found when the repo
// holds no records in the space.
func TestServer_GetLatestCommit_EmptyRepo(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(
		t,
		key,
		store,
	)

	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
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

func TestServer_ListRepoOps_Since(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(
		t,
		key,
		store,
	)

	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")

	_, _, err = store.PutRecord(t.Context(), uri, owner, coll, "k1", mustMarshalRecord(t, map[string]any{"x": 1}))
	require.NoError(t, err)
	_, _, err = store.PutRecord(t.Context(), uri, owner, coll, "k2", mustMarshalRecord(t, map[string]any{"x": 2}))
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
	key, store := newTestStore(t)
	s := newTestServerWithOpts(
		t,
		key,
		store,
	)

	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")

	_, _, err = store.PutRecord(
		t.Context(),
		uri,
		owner,
		coll,
		"k1",
		mustMarshalRecord(t, map[string]any{"text": "hello"}),
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
	key, store := newTestStore(t)
	s := newTestServerWithOpts(
		t,
		key,
		store,
	)

	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")

	_, _, err = store.PutRecord(
		t.Context(),
		uri,
		owner,
		coll,
		"k1",
		mustMarshalRecord(t, map[string]any{"text": "hello"}),
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

// TestServer_ListRepoOpsSinceAheadRejects pins that a since beyond the repo
// head is an error (not an empty page), so an ahead-of-host syncer falls back
// to a full recovery instead of silently stopping.
func TestServer_ListRepoOpsSinceAheadRejects(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(
		t,
		key,
		store,
	)

	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = store.PutRecord(t.Context(), uri, owner, coll, "k1", mustMarshalRecord(t, map[string]any{"v": 1}))
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
