package sap

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type server struct {
	http.ServeMux
	sap *sapImpl
}

var _ http.Handler = (*server)(nil)

func (s *server) handleClientMetadata(w http.ResponseWriter, r *http.Request) {
	cm, err := s.sap.clientMetadata()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(cm); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	stateVal := r.URL.Query().Get("state")
	if code == "" || stateVal == "" {
		http.Error(w, "missing code or state parameter", http.StatusBadRequest)
		return
	}
	org, err := s.sap.completeAuth(r.Context(), code, stateVal)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	slog.InfoContext(r.Context(), "org added", "org", org.DID)

	s.sap.subber.addSubscription(r.Context(), org)
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.HandleFunc("/client-metadata.json", s.handleClientMetadata)
	s.HandleFunc("/oauth-callback", s.handleOAuthCallback)
	http.StripPrefix(s.sap.pathPrefix, &s.ServeMux).ServeHTTP(w, r)
}
