package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/stretchr/testify/require"
)

type fakeOrgResolver struct {
	orgDID string
	err    error
}

func (f *fakeOrgResolver) ResolveCallerOrg(ctx context.Context, bearerToken string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.orgDID, nil
}

type stubIndex struct {
	gotParams QueryParams
	result    QueryResult
}

func (s *stubIndex) Upsert(ctx context.Context, doc Document) error { return nil }
func (s *stubIndex) Delete(ctx context.Context, uri string) error   { return nil }
func (s *stubIndex) Query(ctx context.Context, params QueryParams) (QueryResult, error) {
	s.gotParams = params
	return s.result, nil
}

func TestServer_HandleQuery_FiltersByResolvedOrg(t *testing.T) {
	index := &stubIndex{result: QueryResult{
		Results: []Result{{
			URI:        "ats://did:plc:org1/.../rkey1",
			SpaceURI:   "ats://did:plc:org1/app.space/skey1",
			RecordType: "app.note",
			Snippet:    "<b>budget</b> notes",
			Rank:       0.5,
		}},
	}}
	resolver := &fakeOrgResolver{orgDID: "did:plc:org1"}
	server := NewServer(index, resolver)

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.search.query?q=budget", nil)
	req.Header.Set("Authorization", "Bearer caller-token")
	rec := httptest.NewRecorder()

	server.HandleQuery(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "did:plc:org1", index.gotParams.OrgDID)
	require.Equal(t, "budget", index.gotParams.QueryText)

	var out habitat.NetworkHabitatSearchQueryOutput
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	require.Len(t, out.Results, 1)
	require.Equal(t, "ats://did:plc:org1/.../rkey1", out.Results[0].Uri)
	require.Equal(t, int64(500000), out.Results[0].Rank)
}

func TestServer_HandleQuery_MissingAuthorizationIs401(t *testing.T) {
	server := NewServer(&stubIndex{}, &fakeOrgResolver{orgDID: "did:plc:org1"})

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.search.query?q=budget", nil)
	rec := httptest.NewRecorder()

	server.HandleQuery(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestServer_HandleQuery_MissingQIs400(t *testing.T) {
	server := NewServer(&stubIndex{}, &fakeOrgResolver{orgDID: "did:plc:org1"})

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.search.query", nil)
	req.Header.Set("Authorization", "Bearer caller-token")
	rec := httptest.NewRecorder()

	server.HandleQuery(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestServer_HandleQuery_ResolverErrorIs401(t *testing.T) {
	server := NewServer(&stubIndex{}, &fakeOrgResolver{err: context.DeadlineExceeded})

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.search.query?q=budget", nil)
	req.Header.Set("Authorization", "Bearer caller-token")
	rec := httptest.NewRecorder()

	server.HandleQuery(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}
