package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/habitat-network/habitat/internal/oauth_client"
	"github.com/habitat-network/habitat/internal/sap"
)

type server struct {
	sap         *sap.Sap
	oauthClient *oauth_client.App
}

func NewSapServer(
	sapInstance *sap.Sap,
	oauthClient *oauth_client.App,
) *server {
	return &server{
		sap:         sapInstance,
		oauthClient: oauthClient,
	}
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

	redirectURL, err := s.oauthClient.StartAuthFlow(r.Context(), req.Handle)
	if err != nil {
		http.Error(w, fmt.Sprintf("start auth flow: %s", err), http.StatusInternalServerError)
		return
	}

	if r.Method == http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		err = enc.Encode(map[string]string{"redirect_url": redirectURL})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	w.Header().Set("Location", redirectURL)
	w.WriteHeader(http.StatusSeeOther)
}

func (s *server) handleListOrgs(w http.ResponseWriter, r *http.Request) {
	orgs, err := s.sap.ListManagedOrgs(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if orgs == nil {
		orgs = []sap.OrgInfo{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"orgs": orgs})
}

func (s *server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	sessionData, err := s.oauthClient.ProcessCallback(r.Context(), r.URL.Query())
	if err != nil {
		http.Error(w, fmt.Sprintf("process callback: %s", err), http.StatusInternalServerError)
		return
	}

	if err := s.sap.AddManagedOrg(
		r.Context(),
		sessionData.AccountDID,
		sessionData.SessionID,
	); err != nil {
		http.Error(w, fmt.Sprintf("save org: %s", err), http.StatusInternalServerError)
		return
	}

	slog.InfoContext(r.Context(), "org oauth complete", "did", sessionData.AccountDID)
	w.WriteHeader(http.StatusOK)
	s.sap.StartOrgSync(sessionData.AccountDID)
}

func (s *server) handleClientMetadata(w http.ResponseWriter, r *http.Request) {
	cm := s.oauthClient.Config.ClientMetadata()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(cm); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
