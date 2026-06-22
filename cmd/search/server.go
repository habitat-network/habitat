package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/habitat-network/habitat/api/habitat"
)

type Server struct {
	index      Index
	pearClient PearClient
}

func NewServer(index Index, pearClient PearClient) *Server {
	return &Server{index: index, pearClient: pearClient}
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

	orgDID, err := s.pearClient.ResolveCallerOrg(r.Context(), bearer)
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
