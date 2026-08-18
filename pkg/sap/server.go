package sap

import (
	"encoding/json"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/httpx"
)

type SapServer struct {
	s   *Sap
	mux *http.ServeMux
}

var _ http.Handler = (*SapServer)(nil)

func NewSapServer(s *Sap) *SapServer {
	server := &SapServer{s: s}
	mux := http.NewServeMux()
	mux.HandleFunc("xrpc/network.habitat.space.notifyWrite", server.HandleNotifyWrite)
	mux.HandleFunc("xrpc/network.habitat.space.notifySpaceDeleted", server.HandleNotifySpaceDeleted)
	return server
}

func (s *SapServer) HandleNotifyWrite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSpaceNotifyWriteInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	space, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space")
	if !ok {
		return
	}
	repo, ok := httpx.ParseDIDInput(ctx, w, input.Repo, "repo")
	if !ok {
		return
	}
	rev, err := syntax.ParseTID(input.Rev)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "invalid rev", err)
		return
	}
	if err := s.s.NotifyWrite(ctx, space.URI(), repo, rev, input.Hash); err != nil {
		httpx.WriteServerError(ctx, w, err)
		return
	}
}

func (s *SapServer) HandleNotifySpaceDeleted(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSpaceNotifySpaceDeletedInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	space, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space")
	if !ok {
		return
	}
	if err := s.s.NotifySpaceDeleted(ctx, space.URI()); err != nil {
		httpx.WriteServerError(ctx, w, err)
		return
	}
}

// ServeHTTP implements [http.Handler].
func (s *SapServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}
