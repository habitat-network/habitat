package main

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/habitat-network/habitat/internal/sap"
)

type server struct {
	sap        sap.Sap
	orgManager *orgManager
}

func NewSapServer(sap sap.Sap, o *orgManager) *server {
	s := &server{
		sap:        sap,
		orgManager: o,
	}
	return s
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *server) handleAddOrg(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Handle string `json:"handle"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	redirectURL, err := s.orgManager.InitiateAuth(r.Context(), req.Handle)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if r.Method == http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(map[string]string{"redirect_url": redirectURL})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	w.Header().Set("Location", redirectURL)
	w.WriteHeader(http.StatusSeeOther)
}

func (s *server) handleListOrgs(w http.ResponseWriter, r *http.Request) {
	orgs, err := s.orgManager.GetOrgs(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"orgs": orgs})
}

func (s *server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	stateVal := r.URL.Query().Get("state")
	if code == "" || stateVal == "" {
		http.Error(w, "missing code or state parameter", http.StatusBadRequest)
		return
	}
	org, err := s.orgManager.CompleteAuth(r.Context(), code, stateVal)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	slog.Info("org added", "org", org)
}

func (s *server) handleClientMetadata(w http.ResponseWriter, r *http.Request) {
	cm, err := s.orgManager.ClientMetadata()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cm)
}
