package pearserver_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// typed is the part of a nested lexicon object these tests inspect.
type typed struct {
	Type string `json:"$type"`
}

// TestServer_SpaceEndpointsEncodeRequestNamespace pins that the handlers
// registered under both com.atproto.* and network.habitat.* encode the output
// type matching the namespace the request used, so $type values in the
// response match what the caller asked for.
func TestServer_SpaceEndpointsEncodeRequestNamespace(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	spaceURI, err := ts.SpaceStore.CreateSpace(t.Context(), org, groupTp, "ns")
	require.NoError(t, err)
	_, _, err = ts.SpaceStore.PutRecord(
		t.Context(), spaceURI, owner, "network.habitat.note", "k1",
		spaces_testutil.MustMarshalRecord(t, map[string]any{"x": 1}),
	)
	require.NoError(t, err)
	simpleURI, err := ts.SimpleStore.CreateSpace(
		t.Context(), org, owner, groupTp, habitat_syntax.SpaceKey("simple"),
	)
	require.NoError(t, err)

	query := func(t *testing.T, handler http.HandlerFunc, nsid string, params url.Values) []byte {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/xrpc/"+nsid+"?"+params.Encode(), http.NoBody)
		w := httptest.NewRecorder()
		handler(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		return w.Body.Bytes()
	}

	spaceParams := url.Values{"space": {spaceURI.String()}, "repo": {owner.String()}}

	t.Run("listSpaces", func(t *testing.T) {
		for _, ns := range []string{"com.atproto", "network.habitat"} {
			var out struct {
				Spaces []typed `json:"spaces"`
			}
			body := query(t, ts.Server.ListSpaces, ns+".space.listSpaces", url.Values{})
			require.NoError(t, json.Unmarshal(body, &out))
			require.NotEmpty(t, out.Spaces)
			require.Equal(t, ns+".space.listSpaces#spaceView", out.Spaces[0].Type)
		}
	})

	t.Run("listRepos", func(t *testing.T) {
		for _, ns := range []string{"com.atproto", "network.habitat"} {
			var out struct {
				Repos []typed `json:"repos"`
			}
			body := query(
				t,
				ts.Server.ListRepos,
				ns+".space.listRepos",
				url.Values{"space": {spaceURI.String()}},
			)
			require.NoError(t, json.Unmarshal(body, &out))
			require.NotEmpty(t, out.Repos)
			require.Equal(t, ns+".space.listRepos#repo", out.Repos[0].Type)
		}
	})

	t.Run("listRecords", func(t *testing.T) {
		for _, ns := range []string{"com.atproto", "network.habitat"} {
			var out struct {
				Records []typed `json:"records"`
			}
			body := query(t, ts.Server.ListRecords, ns+".space.listRecords", spaceParams)
			require.NoError(t, json.Unmarshal(body, &out))
			require.NotEmpty(t, out.Records)
			require.Equal(t, ns+".space.listRecords#record", out.Records[0].Type)
		}
	})

	t.Run("listRepoOps", func(t *testing.T) {
		for _, ns := range []string{"com.atproto", "network.habitat"} {
			var out struct {
				Ops    []typed `json:"ops"`
				Commit typed   `json:"commit"`
			}
			body := query(t, ts.Server.ListRepoOps, ns+".space.listRepoOps", spaceParams)
			require.NoError(t, json.Unmarshal(body, &out))
			require.NotEmpty(t, out.Ops)
			require.Equal(t, ns+".space.listRepoOps#opEntry", out.Ops[0].Type)
			require.Equal(t, ns+".space.defs#signedCommit", out.Commit.Type)
		}
	})

	t.Run("getLatestCommit", func(t *testing.T) {
		for _, ns := range []string{"com.atproto", "network.habitat"} {
			var out struct {
				Commit typed `json:"commit"`
			}
			body := query(t, ts.Server.GetLatestCommit, ns+".space.getLatestCommit", spaceParams)
			require.NoError(t, json.Unmarshal(body, &out))
			require.Equal(t, ns+".space.defs#signedCommit", out.Commit.Type)
		}
	})

	t.Run("applyWrites", func(t *testing.T) {
		for _, ns := range []string{"com.atproto", "network.habitat"} {
			input, err := json.Marshal(map[string]any{
				"space": spaceURI.String(),
				"repo":  owner.String(),
				"writes": []any{map[string]any{
					"$type":      ns + ".space.applyWrites#create",
					"collection": "network.habitat.note",
					"rkey":       "ns-" + ns,
					"value":      map[string]any{"x": 2},
				}},
			})
			require.NoError(t, err)
			req := httptest.NewRequest(
				http.MethodPost, "/xrpc/"+ns+".space.applyWrites", bytes.NewReader(input),
			)
			w := httptest.NewRecorder()
			ts.Server.ApplyWrites(w, req)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())

			var out struct {
				Results []typed `json:"results"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
			require.Len(t, out.Results, 1)
			require.Equal(t, ns+".space.applyWrites#createResult", out.Results[0].Type)
		}
	})

	t.Run("simplespace listMembers", func(t *testing.T) {
		params := url.Values{"space": {simpleURI.String()}}
		var out struct {
			Members []typed `json:"members"`
		}
		body := query(t, ts.Server.ListMembers, "com.atproto.simplespace.listMembers", params)
		require.NoError(t, json.Unmarshal(body, &out))
		require.NotEmpty(t, out.Members)
		require.Equal(t, "com.atproto.simplespace.listMembers#member", out.Members[0].Type)

		body = query(t, ts.Server.ListMembers, "network.habitat.simplespace.listMembers", params)
		require.NoError(t, json.Unmarshal(body, &out))
		require.Equal(t, "network.habitat.simplespace.listMembers#member", out.Members[0].Type)
	})
}
