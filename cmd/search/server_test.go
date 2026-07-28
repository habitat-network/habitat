package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/stretchr/testify/require"
)

type stubIndex struct {
	gotParams QueryParams
	result    QueryResult
}

func (s *stubIndex) Upsert(ctx context.Context, doc Document) error { return nil }
func (s *stubIndex) Delete(ctx context.Context, uri habitat_syntax.SpaceRecordURI) error {
	return nil
}
func (s *stubIndex) Query(ctx context.Context, params QueryParams) (QueryResult, error) {
	s.gotParams = params
	return s.result, nil
}

/*
func TestServer_HandleQuery_FiltersByResolvedOrg(t *testing.T) {
	index := &stubIndex{result: QueryResult{
		Results: []Result{{
			URI:        "ats://did:plc:org1/.../rkey1",
			SpaceURI:   "ats://did:plc:org1/app.space/skey1",
			Collection: "network.habitat.note",
			Snippet:    "<b>budget</b> notes",
			Rank:       0.5,
		}},
	}}
	server := NewServer("pear.example.com", index)

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.search.query?q=budget", nil)
	req.Header.Set("Authorization", "Bearer caller-token")
	rec := httptest.NewRecorder()

	server.HandleQuery(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, syntax.DID("did:plc:org1"), index.gotParams.OrgDID)
	require.Equal(t, "budget", index.gotParams.QueryText)

	var out habitat.NetworkHabitatSearchQueryOutput
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	require.Len(t, out.Results, 1)
	require.Equal(t, "ats://did:plc:org1/.../rkey1", out.Results[0].Uri)
	require.Equal(t, int64(500000), out.Results[0].Rank)
}

func TestServer_HandleQuery_MissingAuthorizationIs401(t *testing.T) {
	server := NewServer("pear.example.com", &stubIndex{})

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.search.query?q=budget", nil)
	rec := httptest.NewRecorder()

	server.HandleQuery(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestServer_HandleQuery_MissingQIs400(t *testing.T) {
	server := NewServer("pear.example.com", &stubIndex{})

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.search.query", nil)
	req.Header.Set("Authorization", "Bearer caller-token")
	rec := httptest.NewRecorder()

	server.HandleQuery(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestServer_HandleQuery_ResolverErrorIs401(t *testing.T) {
	server := NewServer("pear.example.com", &stubIndex{})

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.search.query?q=budget", nil)
	req.Header.Set("Authorization", "Bearer caller-token")
	rec := httptest.NewRecorder()

	server.HandleQuery(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestPearClient_ResolveCallerOrg_ForwardsCallerToken(t *testing.T) {
	var gotAuth, gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotMethod = r.Header.Get("Habitat-Auth-Method")
		require.Equal(t, "/xrpc/network.habitat.org.getMetadata", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatOrgGetMetadataOutput{
			OrgId:           "did:plc:org1",
			LoginMethod:     "password",
			HandleSubdomain: "org1",
		})
	}))
	defer server.Close()

	s := NewServer("pear.example.com", &stubIndex{})
	orgDID, err := s.resolveCallerOrg(context.Background(), "callers-own-token")

	require.NoError(t, err)
	require.Equal(t, "did:plc:org1", orgDID.String())
	require.Equal(t, "Bearer callers-own-token", gotAuth)
	require.Equal(t, "oauth", gotMethod)
}
*/

func TestPearClient_ResolveCallerOrg_NonOKStatusIsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	s := NewServer("pear.example.com", &stubIndex{})
	_, err := s.resolveCallerOrg(context.Background(), "callers-own-token")
	require.Error(t, err)
}
