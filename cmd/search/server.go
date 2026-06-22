package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/habitat-network/habitat/api/habitat"
)

// OrgResolver is the subset of pearClient the HTTP handler depends on,
// broken out for testability.
type OrgResolver interface {
	ResolveCallerOrg(ctx context.Context, bearerToken string) (string, error)
}

type Server struct {
	index    Index
	resolver OrgResolver
}

func NewServer(index Index, resolver OrgResolver) *Server {
	return &Server{index: index, resolver: resolver}
}

func (s *Server) HandleQuery(w http.ResponseWriter, r *http.Request) {
	bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if bearer == "" {
		http.Error(w, "missing authorization", http.StatusUnauthorized)
		return
	}

	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, "missing required parameter: q", http.StatusBadRequest)
		return
	}

	orgDID, err := s.resolver.ResolveCallerOrg(r.Context(), bearer)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	limit := 25
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	result, err := s.index.Query(r.Context(), QueryParams{
		OrgDID:    orgDID,
		QueryText: q,
		Limit:     limit,
		Cursor:    r.URL.Query().Get("cursor"),
	})
	if err != nil {
		http.Error(w, "search failed", http.StatusInternalServerError)
		return
	}

	out := habitat.NetworkHabitatSearchQueryOutput{Cursor: result.NextCursor}
	for _, res := range result.Results {
		out.Results = append(out.Results, habitat.NetworkHabitatSearchQueryResultView{
			Uri:        res.URI,
			SpaceUri:   res.SpaceURI,
			RecordType: res.RecordType,
			Snippet:    res.Snippet,
			// Lexicon "rank" is an integer (AT Protocol has no float
			// primitive); scale the float64 rank to preserve precision.
			Rank: int64(res.Rank * 1_000_000),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
