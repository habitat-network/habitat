package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
)

type Server struct {
	// For calling out to pear instance
	pearHost   string
	httpClient *http.Client

	index Index
}

func NewServer(host string, index Index) *Server {
	return &Server{
		pearHost:   host,
		httpClient: &http.Client{},
		index:      index,
	}
}

func (s *Server) resolveCallerOrg(
	ctx context.Context,
	callerBearerToken string,
) (syntax.DID, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, s.pearHost+"/xrpc/network.habitat.org.getMetadata", nil,
	)
	if err != nil {
		return "", fmt.Errorf("build getMetadata request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+callerBearerToken)
	req.Header.Set("Habitat-Auth-Method", "oauth")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("call getMetadata: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("getMetadata returned status %d", resp.StatusCode)
	}
	var out habitat.NetworkHabitatOrgGetMetadataOutput
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode getMetadata response: %w", err)
	}
	return syntax.DID(out.OrgId), nil
}

func (s *Server) HandleQuery(w http.ResponseWriter, r *http.Request) {
	/*
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if bearer == "" {
			http.Error(w, "missing authorization", http.StatusUnauthorized)
			return
		}
	*/

	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, "missing required parameter: q", http.StatusBadRequest)
		return
	}

	/*
		orgDID, err := s.resolveCallerOrg(r.Context(), bearer)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	*/

	limit := 25
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	result, err := s.index.Query(r.Context(), QueryParams{
		OrgDID:    hardcodedOrgDID, // TODO: update
		QueryText: q,
		Limit:     limit,
		Cursor:    r.URL.Query().Get("cursor"),
	})
	if err != nil {
		http.Error(w, "search failed", http.StatusInternalServerError)
		return
	}

	out := habitat.NetworkHabitatSearchQueryOutput{
		Cursor:  result.NextCursor,
		Results: []habitat.NetworkHabitatSearchQueryResultView{},
	}
	for _, res := range result.Results {
		out.Results = append(out.Results, habitat.NetworkHabitatSearchQueryResultView{
			Uri:        res.URI.String(),
			SpaceUri:   res.SpaceURI.String(),
			RecordType: res.Collection.String(),
			Snippet:    res.Snippet,
			// Lexicon "rank" is an integer (AT Protocol has no float
			// primitive); scale the float64 rank to preserve precision.
			Rank: int64(res.Rank * 1_000_000),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
