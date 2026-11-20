package pearserver_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/comatproto"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const noteColl = syntax.NSID("network.habitat.note")

func putNote(
	t *testing.T,
	ts *pearserver_testutil.TestServer,
	uri habitat_syntax.SpaceURI,
	repo syntax.DID,
	rkey syntax.RecordKey,
) {
	t.Helper()
	_, _, err := ts.SpaceStore.PutRecord(
		t.Context(), uri, repo, noteColl, rkey,
		spaces_testutil.MustMarshalRecord(t, map[string]any{"k": rkey.String()}),
	)
	require.NoError(t, err)
}

func repoArchive(
	t *testing.T,
	ts *pearserver_testutil.TestServer,
	uri habitat_syntax.SpaceURI,
	repo syntax.DID,
) []byte {
	t.Helper()
	commit, blocks, err := ts.SpaceStore.RepoSnapshot(t.Context(), uri, repo)
	require.NoError(t, err)
	require.NotNil(t, commit)
	archive, err := spaces.SerializeRepoStar(uri, repo, *commit, blocks)
	require.NoError(t, err)
	return archive
}

func postStar(handler http.HandlerFunc, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/vnd.dasl.star")
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

func TestServer_ClearRecords(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	uri, err := ts.SpaceStore.CreateSpace(t.Context(), org, groupTp, "clear")
	require.NoError(t, err)
	putNote(t, ts, uri, owner, "a")
	putNote(t, ts, uri, owner, "b")
	putNote(t, ts, uri, alice, "x")

	t.Run("rejects clearing another repo", func(t *testing.T) {
		var out struct{}
		code := httpx_testutil.NewTestXRPCClient(t).Procedure(
			ts.Server.ClearRecords,
			comatproto.ComAtprotoSpaceClearRecordsInput{Space: uri.String(), Repo: alice.String()},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)
		left, err := ts.SpaceStore.ListRecords(t.Context(), uri, alice, nil)
		require.NoError(t, err)
		require.Len(t, left, 1)
	})

	t.Run("deletes the caller's records", func(t *testing.T) {
		var out comatproto.ComAtprotoSpaceClearRecordsOutput
		code := httpx_testutil.NewTestXRPCClient(t).Procedure(
			ts.Server.ClearRecords,
			comatproto.ComAtprotoSpaceClearRecordsInput{Space: uri.String(), Repo: owner.String()},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
		left, err := ts.SpaceStore.ListRecords(t.Context(), uri, owner, nil)
		require.NoError(t, err)
		require.Empty(t, left)
		others, err := ts.SpaceStore.ListRecords(t.Context(), uri, alice, nil)
		require.NoError(t, err)
		require.Len(t, others, 1)
	})
}

func TestServer_ImportRepo(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	uri, err := ts.SpaceStore.CreateSpace(t.Context(), org, groupTp, "import")
	require.NoError(t, err)
	putNote(t, ts, uri, owner, "a")
	putNote(t, ts, uri, owner, "b")
	archive := repoArchive(t, ts, uri, owner)
	putNote(t, ts, uri, alice, "x")
	aliceArchive := repoArchive(t, ts, uri, alice)

	t.Run("restores the caller's repo from the archive", func(t *testing.T) {
		putNote(t, ts, uri, owner, "extra")
		w := postStar(ts.Server.ImportRepo, archive)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		records, err := ts.SpaceStore.ListRecords(t.Context(), uri, owner, nil)
		require.NoError(t, err)
		var rkeys []string
		for _, r := range records {
			rkeys = append(rkeys, r.Rkey.String())
		}
		require.ElementsMatch(t, []string{"a", "b"}, rkeys)
	})

	t.Run("refuses another account's repo", func(t *testing.T) {
		w := postStar(ts.Server.ImportRepo, aliceArchive)
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Contains(t, w.Body.String(), "RepoMismatch")
	})

	t.Run("refuses an archive that fails verification", func(t *testing.T) {
		w := postStar(ts.Server.ImportRepo, []byte("not a star archive"))
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Contains(t, w.Body.String(), "InvalidRepoFile")
	})

	t.Run("needs the space to exist", func(t *testing.T) {
		gone, err := ts.SpaceStore.CreateSpace(t.Context(), org, groupTp, "gone")
		require.NoError(t, err)
		putNote(t, ts, gone, owner, "a")
		goneArchive := repoArchive(t, ts, gone, owner)
		require.NoError(t, ts.SpaceStore.DeleteSpace(t.Context(), gone))

		w := postStar(ts.Server.ImportRepo, goneArchive)
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Contains(t, w.Body.String(), "SpaceNotFound")
	})
}

func TestServer_SpaceMetaMigration(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	// The caller is the space's authority, as a migrating account is.
	uri, err := ts.SpaceStore.CreateSpace(t.Context(), owner, groupTp, "meta")
	require.NoError(t, err)
	_, err = ts.PermStore.SetUserRelation(t.Context(), owner, uri, habitat_syntax.SpaceRoleOwner)
	require.NoError(t, err)
	policy := spaces.Policy{Type: spaces.PolicyMemberSpace, PublicRead: true}
	require.NoError(t, ts.SpaceStore.SetSpacePolicy(t.Context(), uri, policy))
	require.NoError(t, ts.SimpleStore.AddMember(t.Context(), uri, alice))
	putNote(t, ts, uri, alice, "x")
	writers := func() map[syntax.DID]spaces.RepoInfo {
		repos, err := ts.SpaceStore.ListRepos(t.Context(), uri)
		require.NoError(t, err)
		byDID := map[syntax.DID]spaces.RepoInfo{}
		for _, r := range repos {
			byDID[r.DID] = r
		}
		return byDID
	}
	// The owner is a writer too: its relation records live in its repo.
	exported := writers()
	require.Contains(t, exported, alice)

	export := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(
			http.MethodGet, "/?space="+uri.String(), http.NoBody,
		)
		w := httptest.NewRecorder()
		ts.Server.ExportSpaceMeta(w, req)
		return w
	}

	w := export()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "application/vnd.dasl.star", w.Header().Get("Content-Type"))
	archive := w.Body.Bytes()

	// Lose the space, as on the old host going away, then restore it.
	require.NoError(t, ts.SimpleStore.DeleteSpace(t.Context(), uri))
	w = postStar(ts.Server.ImportSpaceMeta, archive)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	got, err := ts.SpaceStore.GetSpacePolicy(t.Context(), uri)
	require.NoError(t, err)
	require.Equal(t, policy, got)
	members, err := ts.SimpleStore.ListMembers(t.Context(), uri)
	require.NoError(t, err)
	require.ElementsMatch(t, []syntax.DID{owner, alice}, members)
	restored := writers()
	require.Equal(t, exported[alice].Rev, restored[alice].Rev)
	require.Equal(t, exported[alice].Hash, restored[alice].Hash)
	require.Equal(t, exported[alice].SpaceRev, restored[alice].SpaceRev)
	records, err := ts.SpaceStore.ListRecords(t.Context(), uri, alice, nil)
	require.NoError(t, err)
	require.Empty(t, records, "records arrive with the writer's own importRepo")

	t.Run("importing over an existing space replaces its members", func(t *testing.T) {
		require.NoError(t, ts.SimpleStore.AddMember(t.Context(), uri, "did:plc:late"))
		w := postStar(ts.Server.ImportSpaceMeta, archive)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		members, err := ts.SimpleStore.ListMembers(t.Context(), uri)
		require.NoError(t, err)
		require.ElementsMatch(t, []syntax.DID{owner, alice}, members)
	})

	t.Run("only the authority may create the space", func(t *testing.T) {
		// A space under another account's name, which the caller does not own.
		foreign, err := ts.SimpleStore.CreateSpace(t.Context(), owner, groupTp, "foreign")
		require.NoError(t, err)
		foreignArchive := func() []byte {
			req := httptest.NewRequest(http.MethodGet, "/?space="+foreign.String(), http.NoBody)
			w := httptest.NewRecorder()
			ts.Server.ExportSpaceMeta(w, req)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			return w.Body.Bytes()
		}()
		require.NoError(t, ts.SimpleStore.DeleteSpace(t.Context(), foreign))

		w := postStar(ts.Server.ImportSpaceMeta, foreignArchive)
		require.Equal(t, http.StatusForbidden, w.Code)
		require.Contains(t, w.Body.String(), "NotAuthorized")
		ok, err := ts.SpaceStore.CheckSpaceExists(t.Context(), foreign)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("refuses an archive that fails verification", func(t *testing.T) {
		w := postStar(ts.Server.ImportSpaceMeta, []byte("not a star archive"))
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Contains(t, w.Body.String(), "InvalidSpaceMeta")
	})
}
