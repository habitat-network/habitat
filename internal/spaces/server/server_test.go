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
	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipld/go-car"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/pearsetup/testutil"
	"github.com/habitat-network/habitat/internal/spacecommit"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

var (
	// owner is an external (non-hive-hosted) DID, so records it writes are
	// signed with the harness's host key rather than a hive member key —
	// which is what the commit-signature assertions below check against.
	owner     = syntax.DID("did:plc:owner")
	alice     = syntax.DID("did:plc:alice")
	groupType = syntax.NSID("network.habitat.group")
)

// newTestPear returns a harness and an actor for owner, scoped to a fresh
// org. owner never needs real org membership for these tests — its access
// comes entirely from the explicit space grants newSpace makes — so its
// token is minted directly rather than going through NewOrg/NewMember.
func newTestPear(t *testing.T) (*testutil.TestPear, *testutil.Actor) {
	t.Helper()
	p := testutil.New(t)
	admin := p.NewOrg("acme")
	return p, p.ActorWithScopes(owner, admin.Org, "org:*")
}

// newSpace creates a space owned by actor.Org and grants actor the owner role
// on it: spaces.Store.CreateSpace itself writes no FGA tuple, so a space's
// creator only has rights on it once one is granted explicitly.
func newSpace(
	t *testing.T,
	p *testutil.TestPear,
	actor *testutil.Actor,
	spaceType syntax.NSID,
	skey string,
) habitat_syntax.SpaceURI {
	t.Helper()
	uri, err := p.SpacesStore.CreateSpace(
		t.Context(), actor.Org, actor.DID, spaceType, habitat_syntax.SpaceKey(skey),
	)
	require.NoError(t, err)
	_, err = p.PermStore.SetUserRelation(t.Context(), actor.DID, uri, habitat_syntax.SpaceRoleOwner)
	require.NoError(t, err)
	return uri
}

// decodeBody decodes a response body regardless of status, for assertions
// Query/Procedure's automatic 200-only decoding doesn't cover.
func decodeBody(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	require.NoError(t, json.NewDecoder(resp.Body).Decode(out))
}

func TestServer(t *testing.T) {
	ctx := t.Context()
	p := testutil.New(t)
	org := p.NewOrg("test-org")
	alice := p.NewMember(org, "alice")
	bob := p.NewMember(org, "bob")
	space1, err := p.SimpleSpaceStore.CreateSpace(
		ctx,
		org.DID,
		alice.DID,
		"test.space.type",
		"",
	)
	require.NoError(t, err)
	space2, err := p.SimpleSpaceStore.CreateSpace(
		ctx,
		org.DID,
		bob.DID,
		"test.space.type",
		"",
	)
	require.NoError(t, err)
	space3, err := p.SimpleSpaceStore.CreateSpace(
		ctx,
		org.DID,
		bob.DID,
		"another.space.type",
		"",
	)
	p.XRPCProcedure("network.habitat.spaces.putRecord"
		habitat.NetworkHabitatSpacePutRecordInput{
		Collection: ,
	}

		)
	require.NoError(t, err)

	t.Run("uploadBlob and getBlob", func(t *testing.T) {
		var uploadBlobResp habitat.NetworkHabitatRepoUploadBlobOutput
		p.XRPC(
			httptest.NewRequest(
				http.MethodPost,
				"/xrpc/network.habitat.repo.uploadBlob",
				strings.NewReader("hello blobs"),
			),
			&uploadBlobResp,
			testutil.WithHeader("Content-Type", "text/plain"),
			testutil.WithOAuth(alice.DID),
		)
		getBlobReq, err := json.Marshal(habitat.NetworkHabitatSpaceGetBlobParams{
			Space: space1.String(),
			Cid:   uploadBlobResp.Cid,
		})
		require.NoError(t, err)
		getBlobResp := p.Do(
			httptest.NewRequest(
				http.MethodGet,
				"/xrpc/network.habitat.space.getBlob",
				bytes.NewReader(getBlobReq),
			),
			testutil.WithOAuth(alice.DID),
		)
		require.Equal(t, []byte("hello blobs"), getBlobResp.Body.Bytes())
	})
	t.Run("uploadBlob rejects oversized", func(t *testing.T) {
		oversized := make([]byte, 500*1024+1)
		var uploadBlobResp atclient.ErrorBody
		p.XRPC(
			httptest.NewRequest(
				http.MethodPost,
				"/xrpc/network.habitat.repo.uploadBlob",
				bytes.NewReader(oversized),
			),
			&uploadBlobResp,
			testutil.WithHeader("Content-Type", "text/plain"),
			testutil.WithOAuth("did:web:test.com"),
		)
		require.Equal(t, "BlobTooLarge", uploadBlobResp.Name)
	})

	t.Run("listSpaces", func(t *testing.T) {
		t.Run("no filter", func(t *testing.T) {
			var listSpacsResp habitat.NetworkHabitatSpaceListSpacesOutput
			p.XRPCQuery(
				"network.habitat.space.listSpaces",
				url.Values{},
				&listSpacsResp,
				testutil.WithOAuth(alice.DID),
			)
			require.Len(t, listSpacsResp.Spaces, 1)
			require.Equal(t, space1.String(), listSpacsResp.Spaces[0])
		})
	})
}

func TestServer_ListRepos(t *testing.T) {
	p, ownerActor := newTestPear(t)
	uri := newSpace(t, p, ownerActor, groupType, "shared")

	coll := syntax.NSID("network.habitat.note")
	_, _, err := p.SpacesStore.PutRecord(
		t.Context(),
		uri,
		owner,
		coll,
		"k1",
		map[string]any{"x": 1},
	)
	require.NoError(t, err)

	var out habitat.NetworkHabitatSpaceListReposOutput
	resp := p.Query(
		ownerActor,
		"network.habitat.space.listRepos",
		url.Values{"space": {uri.String()}},
		&out,
	)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	// 2, not 1: newSpace's own fixture grant (writing the owner relation) is
	// authored under the space's own owner DID, so it shows up as a second
	// repo alongside the one owner itself wrote to.
	require.Len(t, out.Repos, 2)
	var ownerRepo *habitat.NetworkHabitatSpaceListReposRepo
	for i, r := range out.Repos {
		if r.Did == "did:plc:owner" {
			ownerRepo = &out.Repos[i]
		}
	}
	require.NotNil(t, ownerRepo, "did:plc:owner must be among the repos")
	require.NotEmpty(t, ownerRepo.Rev)
	// The repo's LtHash commit hash is populated.
	require.NotEmpty(t, ownerRepo.Hash)
}

func TestServer_PutAndGetRecord(t *testing.T) {
	p, ownerActor := newTestPear(t)
	uri := newSpace(t, p, ownerActor, groupType, "test")

	var putOutput habitat.NetworkHabitatSpacePutRecordOutput
	putResp := p.Procedure(ownerActor, "network.habitat.space.putRecord", map[string]any{
		"space":      uri.String(),
		"repo":       owner.String(),
		"collection": "network.habitat.note",
		"rkey":       "my-note",
		"record":     map[string]any{"text": "hello"},
	}, &putOutput)
	require.Equal(t, http.StatusOK, putResp.StatusCode)
	require.Contains(t, putOutput.Uri, "/network.habitat.note/my-note")

	var getOutput habitat.NetworkHabitatSpaceGetRecordOutput
	getResp := p.Query(ownerActor, "network.habitat.space.getRecord", url.Values{
		"space": {uri.String()}, "collection": {"network.habitat.note"},
		"rkey": {"my-note"}, "repo": {owner.String()},
	}, &getOutput)
	require.Equal(t, http.StatusOK, getResp.StatusCode)
	require.Equal(t, putOutput.Uri, getOutput.Uri)
	val, ok := getOutput.Value.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "hello", val["text"])
}

func TestServer_DeleteRecord(t *testing.T) {
	p, ownerActor := newTestPear(t)
	uri := newSpace(t, p, ownerActor, groupType, "test")

	_, _, err := p.SpacesStore.PutRecord(
		t.Context(),
		uri,
		owner,
		syntax.NSID("network.habitat.note"),
		"del-me",
		map[string]any{"x": 1},
	)
	require.NoError(t, err)

	resp := p.Procedure(ownerActor, "network.habitat.space.deleteRecord", map[string]any{
		"space": uri.String(), "repo": owner.String(),
		"collection": "network.habitat.note", "rkey": "del-me",
	}, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	_, err = p.SpacesStore.GetRecord(
		t.Context(),
		uri,
		owner,
		syntax.NSID("network.habitat.note"),
		"del-me",
	)
	require.ErrorIs(t, err, spaces.ErrRecordNotFound)
}

func TestServer_ListRecords(t *testing.T) {
	p, ownerActor := newTestPear(t)
	uri := newSpace(t, p, ownerActor, groupType, "test")

	coll := syntax.NSID("network.habitat.note")
	_, _, err := p.SpacesStore.PutRecord(
		t.Context(),
		uri,
		owner,
		coll,
		"k1",
		map[string]any{"x": 1},
	)
	require.NoError(t, err)
	_, _, err = p.SpacesStore.PutRecord(t.Context(), uri, owner, coll, "k2", map[string]any{"x": 2})
	require.NoError(t, err)

	var out habitat.NetworkHabitatSpaceListRecordsOutput
	resp := p.Query(ownerActor, "network.habitat.space.listRecords", url.Values{
		"space": {uri.String()}, "collection": {"network.habitat.note"}, "repo": {owner.String()},
	}, &out)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, out.Records, 2)
	require.Equal(t, "k1", out.Records[0].Rkey)
	require.Equal(t, "k2", out.Records[1].Rkey)
}

// TestServer_GetRepo verifies getRepo returns a CAR whose first root is a real
// signed commit over the repo's LtHash, verifiable against the host key.
func TestServer_GetRepo(t *testing.T) {
	p, ownerActor := newTestPear(t)
	pub, err := p.HostKey.PublicKey()
	require.NoError(t, err)

	uri := newSpace(t, p, ownerActor, groupType, "test")
	coll := syntax.NSID("network.habitat.note")
	_, _, err = p.SpacesStore.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)

	resp := p.Do(ownerActor, httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.getRepo?space="+uri.String()+"&repo="+owner.String(),
		http.NoBody,
	))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "application/vnd.ipld.car", resp.Header.Get("Content-Type"))

	carBytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	reader, err := car.NewCarReader(bytes.NewReader(carBytes))
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
	_, wantHash, found, err := p.SpacesStore.RepoHead(t.Context(), uri, owner)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, wantHash, []byte(hash))
}

func TestServer_GetRepo_RepoNotFound(t *testing.T) {
	p, ownerActor := newTestPear(t)
	uri := newSpace(t, p, ownerActor, groupType, "test")

	resp := p.Do(ownerActor, httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.getRepo?space="+uri.String()+"&repo="+alice.String(),
		http.NoBody,
	))

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"error":"RepoNotFound"}`, string(body))
}

// TestServer_GetRepo_SpaceNotFound distinguishes an unknown space (400,
// SpaceNotFound) from a known space with no repo (404, RepoNotFound, above).
func TestServer_GetRepo_SpaceNotFound(t *testing.T) {
	p, ownerActor := newTestPear(t)
	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")

	resp := p.Do(ownerActor, httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.getRepo?space="+uri.String()+"&repo="+owner.String(),
		http.NoBody,
	))

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"error":"SpaceNotFound"}`, string(body))
}

func TestServer_PutRecord_Unauthorized(t *testing.T) {
	p, ownerActor := newTestPear(t)
	uri := newSpace(t, p, ownerActor, groupType, "test")

	resp := p.Procedure(ownerActor, "network.habitat.space.putRecord", map[string]any{
		"space": uri.String(), "repo": alice.String(),
		"collection": "network.habitat.note", "rkey": "test", "record": map[string]any{"x": 1},
	}, nil)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestServer_DeleteRecord_Unauthorized(t *testing.T) {
	p, ownerActor := newTestPear(t)
	uri := newSpace(t, p, ownerActor, groupType, "test")

	_, _, err := p.SpacesStore.PutRecord(
		t.Context(),
		uri,
		owner,
		syntax.NSID("network.habitat.note"),
		"test",
		map[string]any{"x": 1},
	)
	require.NoError(t, err)

	resp := p.Procedure(ownerActor, "network.habitat.space.deleteRecord", map[string]any{
		"space": uri.String(), "repo": alice.String(),
		"collection": "network.habitat.note", "rkey": "test",
	}, nil)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestServer_ListRepoOps(t *testing.T) {
	p, ownerActor := newTestPear(t)
	uri := newSpace(t, p, ownerActor, groupType, "test")
	coll := syntax.NSID("network.habitat.note")

	_, _, err := p.SpacesStore.PutRecord(
		t.Context(),
		uri,
		owner,
		coll,
		"k1",
		map[string]any{"x": 1},
	)
	require.NoError(t, err)
	_, _, err = p.SpacesStore.PutRecord(t.Context(), uri, owner, coll, "k2", map[string]any{"x": 2})
	require.NoError(t, err)

	var out habitat.NetworkHabitatSpaceListRepoOpsOutput
	resp := p.Query(ownerActor, "network.habitat.space.listRepoOps", url.Values{
		"space": {uri.String()}, "repo": {owner.String()},
	}, &out)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, out.Ops, 2)
	require.Equal(t, "k1", out.Ops[0].Rkey)
	require.Equal(t, "k2", out.Ops[1].Rkey)
	require.Equal(t, coll.String(), out.Ops[0].Collection)
	require.NotEmpty(t, out.Ops[0].Rev)
	require.NotEmpty(t, out.Cursor)
	require.Equal(t, out.Ops[1].Rev, out.Cursor)
}

// TestServer_ListRepoOps_IncludesSignedCommit verifies that at the head of the
// oplog a host-signed commit is returned, and that it verifies against the host
// key with the host protocol tag and carries the repo's LtHash.
func TestServer_ListRepoOps_IncludesSignedCommit(t *testing.T) {
	p, ownerActor := newTestPear(t)
	pub, err := p.HostKey.PublicKey()
	require.NoError(t, err)

	uri := newSpace(t, p, ownerActor, groupType, "test")
	_, _, err = p.SpacesStore.PutRecord(
		t.Context(),
		uri,
		owner,
		groupType,
		"k1",
		map[string]any{"x": 1},
	)
	require.NoError(t, err)

	var out habitat.NetworkHabitatSpaceListRepoOpsOutput
	resp := p.Query(ownerActor, "network.habitat.space.listRepoOps", url.Values{
		"space": {uri.String()}, "repo": {owner.String()},
	}, &out)
	require.Equal(t, http.StatusOK, resp.StatusCode)
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

	_, wantHash, found, err := p.SpacesStore.RepoHead(t.Context(), uri, owner)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, wantHash, hash)
}

// TestServer_GetLatestCommit returns a host-signed commit over the repo's head
// that verifies against the host key and carries the repo's LtHash.
func TestServer_GetLatestCommit(t *testing.T) {
	p, ownerActor := newTestPear(t)
	pub, err := p.HostKey.PublicKey()
	require.NoError(t, err)

	uri := newSpace(t, p, ownerActor, groupType, "test")
	_, _, err = p.SpacesStore.PutRecord(
		t.Context(),
		uri,
		owner,
		groupType,
		"k1",
		map[string]any{"x": 1},
	)
	require.NoError(t, err)

	var out habitat.NetworkHabitatSpaceGetLatestCommitOutput
	resp := p.Query(ownerActor, "network.habitat.space.getLatestCommit", url.Values{
		"space": {uri.String()}, "repo": {owner.String()},
	}, &out)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, int64(spacecommit.Version), out.Commit.Ver)

	hash := []byte(out.Commit.Hash)
	ikm := []byte(out.Commit.Ikm)
	sig := []byte(out.Commit.Sig)
	require.Len(t, ikm, 32)

	ctxBytes := spacecommit.Ctx(uri, owner, out.Commit.Rev, ikm)
	require.NoError(t, pub.HashAndVerify(ctxBytes, sig))

	rev, wantHash, found, err := p.SpacesStore.RepoHead(t.Context(), uri, owner)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, wantHash, hash)
	require.Equal(t, rev, out.Commit.Rev)
}

// TestServer_GetLatestCommit_EmptyRepo returns repo-not-found when the repo
// holds no records in the space.
func TestServer_GetLatestCommit_EmptyRepo(t *testing.T) {
	p, ownerActor := newTestPear(t)
	uri := newSpace(t, p, ownerActor, groupType, "test")

	resp := p.Query(ownerActor, "network.habitat.space.getLatestCommit", url.Values{
		"space": {uri.String()}, "repo": {owner.String()},
	}, nil)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestServer_ListRepoOps_Since(t *testing.T) {
	p, ownerActor := newTestPear(t)
	uri := newSpace(t, p, ownerActor, groupType, "test")
	coll := syntax.NSID("network.habitat.note")

	_, _, err := p.SpacesStore.PutRecord(
		t.Context(),
		uri,
		owner,
		coll,
		"k1",
		map[string]any{"x": 1},
	)
	require.NoError(t, err)
	_, _, err = p.SpacesStore.PutRecord(t.Context(), uri, owner, coll, "k2", map[string]any{"x": 2})
	require.NoError(t, err)

	var first habitat.NetworkHabitatSpaceListRepoOpsOutput
	p.Query(ownerActor, "network.habitat.space.listRepoOps",
		url.Values{"space": {uri.String()}, "repo": {owner.String()}}, &first)
	require.Len(t, first.Ops, 2)

	var second habitat.NetworkHabitatSpaceListRepoOpsOutput
	resp := p.Query(ownerActor, "network.habitat.space.listRepoOps", url.Values{
		"space": {uri.String()}, "repo": {owner.String()}, "since": {first.Cursor},
	}, &second)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, second.Ops, 0)
}

func TestServer_ListRepoOps_IncludesValue(t *testing.T) {
	p, ownerActor := newTestPear(t)
	uri := newSpace(t, p, ownerActor, groupType, "test")
	coll := syntax.NSID("network.habitat.note")

	_, _, err := p.SpacesStore.PutRecord(
		t.Context(),
		uri,
		owner,
		coll,
		"k1",
		map[string]any{"text": "hello"},
	)
	require.NoError(t, err)

	var out habitat.NetworkHabitatSpaceListRepoOpsOutput
	resp := p.Query(ownerActor, "network.habitat.space.listRepoOps",
		url.Values{"space": {uri.String()}, "repo": {owner.String()}}, &out)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, out.Ops, 1)
	val, ok := out.Ops[0].Value.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "hello", val["text"])
}

func TestServer_ListRepoOps_ExcludeValues(t *testing.T) {
	p, ownerActor := newTestPear(t)
	uri := newSpace(t, p, ownerActor, groupType, "test")
	coll := syntax.NSID("network.habitat.note")

	_, _, err := p.SpacesStore.PutRecord(
		t.Context(),
		uri,
		owner,
		coll,
		"k1",
		map[string]any{"text": "hello"},
	)
	require.NoError(t, err)

	var out habitat.NetworkHabitatSpaceListRepoOpsOutput
	resp := p.Query(ownerActor, "network.habitat.space.listRepoOps", url.Values{
		"space": {uri.String()}, "repo": {owner.String()}, "excludeValues": {"true"},
	}, &out)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, out.Ops, 1)
	require.Equal(t, "k1", out.Ops[0].Rkey)
	require.Nil(t, out.Ops[0].Value)
}

// TestServer_ListRepoOpsSinceAheadRejects pins that a since beyond the repo
// head is an error (not an empty page), so an ahead-of-host syncer falls back
// to a full recovery instead of silently stopping.
func TestServer_ListRepoOpsSinceAheadRejects(t *testing.T) {
	p, ownerActor := newTestPear(t)
	uri := newSpace(t, p, ownerActor, groupType, "test")
	coll := syntax.NSID("network.habitat.note")

	_, _, err := p.SpacesStore.PutRecord(
		t.Context(),
		uri,
		owner,
		coll,
		"k1",
		map[string]any{"v": 1},
	)
	require.NoError(t, err)

	// A TID-like string that sorts after any real TID (base32 is a-z + 2-7).
	ahead := strings.Repeat("z", 13)
	resp := p.Query(ownerActor, "network.habitat.space.listRepoOps", url.Values{
		"space": {uri.String()}, "repo": {owner.String()}, "since": {ahead},
	}, nil)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var body atclient.ErrorBody
	decodeBody(t, resp, &body)
	require.Equal(t, "RevNotFound", body.Name)
}
