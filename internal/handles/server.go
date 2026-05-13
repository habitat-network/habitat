package handles

import (
	"encoding/json"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/utils"
)

type Server struct {
	handles Handles
	auth    authn.Method
}

func NewServer(
	handles Handles,
) (*Server, error) {
	return &Server{handles: handles}, nil
}

func (s *Server) ServeHandle(w http.ResponseWriter, r *http.Request) {
	handle, err := syntax.ParseHandle(r.Host)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	ident, err := s.handles.LookupHandle(r.Context(), handle)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(ident.DID.String()))
}

func (s *Server) MintHandle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// TODO add auth

	var req struct {
		Handle string `json:"handle"`
		DID    string `json:"did"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteHTTPError(w, err, http.StatusBadRequest)
		return
	}

	did, err := syntax.ParseDID(req.DID)
	if err != nil {
		utils.WriteHTTPError(w, err, http.StatusBadRequest)
		return
	}

	if err := s.handles.MintHandle(r.Context(), req.Handle, did); err != nil {
		utils.WriteHTTPError(w, err, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}
