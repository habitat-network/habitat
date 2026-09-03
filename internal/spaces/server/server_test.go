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
	"github.com/habitat-network/habitat/internal/clientmeta"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/hive"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
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
		clientmeta.NewResolver(),
	)
}

// TestServer_UploadAndGetBlob exercises the raw-body upload/download endpoints,
// which carry non-JSON payloads and thus can't go through TestXRPCClient.
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
	_, _, err = store.PutRecord(
		t.Context(),
		uri,
		owner,
		coll,
		"k1",
		spaces_testutil.MustMarshalRecord(t, map[string]any{"x": 1}),
	)
	require.NoError(t, err)

	var output habitat.NetworkHabitatSpaceListSpacesOutput
	code := httpx_testutil.NewTestXRPCClient(t).Query(s.ListSpaces, url.Values{}, &output)

	require.Equal(t, http.StatusOK, code)
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
	_, _, err = store.PutRecord(
		t.Context(),
		uri,
		owner,
		coll,
		"k1",
		spaces_testutil.MustMarshalRecord(t, map[string]any{"x": 1}),
	)
	require.NoError(t, err)

	var output habitat.NetworkHabitatSpaceListReposOutput
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		s.ListRepos, url.Values{"space": {uri.String()}}, &output,
	)

	require.Equal(t, http.StatusOK, code)
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

	client := httpx_testutil.NewTestXRPCClient(t)

	var putOutput habitat.NetworkHabitatSpacePutRecordOutput
	putCode := client.Procedure(
		s.PutRecord,
		habitat.NetworkHabitatSpacePutRecordInput{
			Space: uri.String(), Repo: "did:plc:owner",
			Collection: "network.habitat.note", Rkey: "my-note",
			Record: map[string]any{"text": "hello"},
		},
		&putOutput,
	)
	require.Equal(t, http.StatusOK, putCode)
	require.Contains(t, putOutput.Uri, "/network.habitat.note/my-note")

	var getOutput habitat.NetworkHabitatSpaceGetRecordOutput
	getCode := client.Query(
		s.GetRecord,
		url.Values{
			"space": {uri.String()}, "collection": {"network.habitat.note"},
			"rkey": {"my-note"}, "repo": {"did:plc:owner"},
		},
		&getOutput,
	)

	require.Equal(t, http.StatusOK, getCode)
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
		spaces_testutil.MustMarshalRecord(t, map[string]any{"x": 1}),
	)
	require.NoError(t, err)

	var out habitat.NetworkHabitatSpaceDeleteRecordOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.DeleteRecord,
		habitat.NetworkHabitatSpaceDeleteRecordInput{
			Space: uri.String(), Repo: "did:plc:owner",
			Collection: "network.habitat.note", Rkey: "del-me",
		},
		&out,
	)
	require.Equal(t, http.StatusOK, code)

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
	_, _, err = store.PutRecord(
		t.Context(),
		uri,
		owner,
		coll,
		"k1",
		spaces_testutil.MustMarshalRecord(t, map[string]any{"x": 1}),
	)
	require.NoError(t, err)
	_, _, err = store.PutRecord(
		t.Context(),
		uri,
		owner,
		coll,
		"k2",
		spaces_testutil.MustMarshalRecord(t, map[string]any{"x": 2}),
	)
	require.NoError(t, err)

	var output habitat.NetworkHabitatSpaceListRecordsOutput
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		s.ListRecords,
		url.Values{
			"space": {
				uri.String(),
			}, "collection": {"network.habitat.note"}, "repo": {owner.String()},
		},
		&output,
	)

	require.Equal(t, http.StatusOK, code)
	require.Len(t, output.Records, 2)
	require.Equal(t, "k1", output.Records[0].Rkey)
	require.Equal(t, "k2", output.Records[1].Rkey)
}

// TestServer_GetRepo verifies getRepo returns a CAR whose first root is a real
// signed commit over the repo's LtHash, verifiable against the host key. It
// carries a non-JSON (CAR) body, so it can't go through TestXRPCClient.
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
	_, _, err = store.PutRecord(
		t.Context(),
		uri,
		owner,
		coll,
		"k1",
		spaces_testutil.MustMarshalRecord(t, map[string]any{"x": 1}),
	)
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

	var apiErr atclient.ErrorBody
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		s.GetRepo, url.Values{"space": {uri.String()}, "repo": {alice.String()}}, &apiErr,
	)

	require.Equal(t, http.StatusNotFound, code)
	require.Equal(t, "RepoNotFound", apiErr.Name)
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

	var apiErr atclient.ErrorBody
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		s.GetRepo, url.Values{"space": {uri.String()}, "repo": {owner.String()}}, &apiErr,
	)

	require.Equal(t, http.StatusBadRequest, code)
	require.Equal(t, "SpaceNotFound", apiErr.Name)
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

	var out struct{}
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.PutRecord,
		habitat.NetworkHabitatSpacePutRecordInput{
			Space: uri.String(), Repo: "did:plc:alice",
			Collection: "network.habitat.note", Rkey: "test",
			Record: map[string]any{"x": 1},
		},
		&out,
	)
	require.Equal(t, http.StatusBadRequest, code)
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

	var out struct{}
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.PutRecord,
		habitat.NetworkHabitatSpacePutRecordInput{
			Space: uri.String(), Repo: "did:plc:owner",
			Collection: "network.habitat.note", Rkey: "my-note",
			Record: map[string]any{"x": 0.15},
		},
		&out,
	)

	require.Equal(t, http.StatusBadRequest, code)

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
		spaces_testutil.MustMarshalRecord(t, map[string]any{"x": 1}),
	)
	require.NoError(t, err)

	var out struct{}
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.DeleteRecord,
		habitat.NetworkHabitatSpaceDeleteRecordInput{
			Space: uri.String(), Repo: "did:plc:alice",
			Collection: "network.habitat.note", Rkey: "test",
		},
		&out,
	)
	require.Equal(t, http.StatusBadRequest, code)
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

	_, _, err = store.PutRecord(
		t.Context(),
		uri,
		owner,
		coll,
		"k1",
		spaces_testutil.MustMarshalRecord(t, map[string]any{"x": 1}),
	)
	require.NoError(t, err)
	_, _, err = store.PutRecord(
		t.Context(),
		uri,
		owner,
		coll,
		"k2",
		spaces_testutil.MustMarshalRecord(t, map[string]any{"x": 2}),
	)
	require.NoError(t, err)

	var output habitat.NetworkHabitatSpaceListRepoOpsOutput
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		s.ListRepoOps, url.Values{"space": {uri.String()}, "repo": {"did:plc:owner"}}, &output,
	)
	require.Equal(t, http.StatusOK, code)

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
	_, _, err = store.PutRecord(
		t.Context(),
		uri,
		owner,
		groupType,
		"k1",
		spaces_testutil.MustMarshalRecord(t, map[string]any{"x": 1}),
	)
	require.NoError(t, err)

	var out habitat.NetworkHabitatSpaceListRepoOpsOutput
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		s.ListRepoOps, url.Values{"space": {uri.String()}, "repo": {owner.String()}}, &out,
	)
	require.Equal(t, http.StatusOK, code)

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
	_, _, err = store.PutRecord(
		t.Context(),
		uri,
		owner,
		groupType,
		"k1",
		spaces_testutil.MustMarshalRecord(t, map[string]any{"x": 1}),
	)
	require.NoError(t, err)

	var out habitat.NetworkHabitatSpaceGetLatestCommitOutput
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		s.GetLatestCommit, url.Values{"space": {uri.String()}, "repo": {owner.String()}}, &out,
	)
	require.Equal(t, http.StatusOK, code)

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

	var out struct{}
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		s.GetLatestCommit, url.Values{"space": {uri.String()}, "repo": {owner.String()}}, &out,
	)
	require.Equal(t, http.StatusNotFound, code)
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

	_, _, err = store.PutRecord(
		t.Context(),
		uri,
		owner,
		coll,
		"k1",
		spaces_testutil.MustMarshalRecord(t, map[string]any{"x": 1}),
	)
	require.NoError(t, err)
	_, _, err = store.PutRecord(
		t.Context(),
		uri,
		owner,
		coll,
		"k2",
		spaces_testutil.MustMarshalRecord(t, map[string]any{"x": 2}),
	)
	require.NoError(t, err)

	client := httpx_testutil.NewTestXRPCClient(t)

	var first habitat.NetworkHabitatSpaceListRepoOpsOutput
	client.Query(
		s.ListRepoOps, url.Values{"space": {uri.String()}, "repo": {"did:plc:owner"}}, &first,
	)
	require.Len(t, first.Ops, 2)

	var second habitat.NetworkHabitatSpaceListRepoOpsOutput
	client.Query(
		s.ListRepoOps,
		url.Values{"space": {uri.String()}, "repo": {"did:plc:owner"}, "since": {first.Cursor}},
		&second,
	)
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
		spaces_testutil.MustMarshalRecord(t, map[string]any{"text": "hello"}),
	)
	require.NoError(t, err)

	var output habitat.NetworkHabitatSpaceListRepoOpsOutput
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		s.ListRepoOps, url.Values{"space": {uri.String()}, "repo": {"did:plc:owner"}}, &output,
	)
	require.Equal(t, http.StatusOK, code)

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
		spaces_testutil.MustMarshalRecord(t, map[string]any{"text": "hello"}),
	)
	require.NoError(t, err)

	var output habitat.NetworkHabitatSpaceListRepoOpsOutput
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		s.ListRepoOps,
		url.Values{
			"space": {uri.String()}, "repo": {"did:plc:owner"}, "excludeValues": {"true"},
		},
		&output,
	)
	require.Equal(t, http.StatusOK, code)

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
	_, _, err = store.PutRecord(
		t.Context(),
		uri,
		owner,
		coll,
		"k1",
		spaces_testutil.MustMarshalRecord(t, map[string]any{"v": 1}),
	)
	require.NoError(t, err)

	// A TID-like string that sorts after any real TID (base32 is a-z + 2-7).
	ahead := strings.Repeat("z", 13)
	var body atclient.ErrorBody
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		s.ListRepoOps,
		url.Values{"space": {uri.String()}, "repo": {owner.String()}, "since": {ahead}},
		&body,
	)
	require.Equal(t, http.StatusBadRequest, code)
	require.Equal(t, "RevNotFound", body.Name)
}
