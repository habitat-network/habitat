package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/habitat-network/habitat/internal/sap"
)

// serviceID is the fragment in the home server's did:web service entry. The
// frontend targets it via the Atproto-Proxy header (did:web:<domain>#groups) so
// pear forwards network.habitat.groups.* calls here.
const serviceID = "groups"

type Server struct {
	domain      string
	orgHandle   string
	groups      *GroupService
	oauthApp    *oauthclient.App
	sap         *sap.Sap
	store       *Store
	serviceAuth authn.Method
}

func NewServer(
	domain, orgHandle string,
	groups *GroupService,
	oauthApp *oauthclient.App,
	s *sap.Sap,
	store *Store,
	serviceAuth authn.Method,
) *Server {
	return &Server{
		domain:      domain,
		orgHandle:   orgHandle,
		groups:      groups,
		oauthApp:    oauthApp,
		sap:         s,
		store:       store,
		serviceAuth: serviceAuth,
	}
}

func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /.well-known/did.json", s.handleDIDDoc)
	mux.HandleFunc("GET /client-metadata.json", s.handleClientMetadata)
	mux.HandleFunc("GET /oauth/login", s.handleOAuthLogin)
	mux.HandleFunc("GET /oauth-callback", s.handleOAuthCallback)

	mux.HandleFunc("GET /xrpc/network.habitat.groups.listGroups", s.handleListGroups)
	mux.HandleFunc("GET /xrpc/network.habitat.groups.getGroup", s.handleGetGroup)
	mux.HandleFunc("POST /xrpc/network.habitat.groups.createGroup", s.handleCreateGroup)
	mux.HandleFunc("POST /xrpc/network.habitat.groups.updateGroup", s.handleUpdateGroup)
	mux.HandleFunc("POST /xrpc/network.habitat.groups.addMember", s.handleAddMember)
	mux.HandleFunc("POST /xrpc/network.habitat.groups.deleteMember", s.handleDeleteMember)
}

// handleDIDDoc serves the did:web document. pear resolves did:web:<domain> here
// and reads the #groups service endpoint to forward calls to this server.
func (s *Server) handleDIDDoc(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, map[string]any{
		"@context": []string{"https://www.w3.org/ns/did/v1"},
		"id":       "did:web:" + s.domain,
		"service": []map[string]any{
			{
				"id":              "#" + serviceID,
				"type":            "HabitatGroupsServer",
				"serviceEndpoint": "https://" + s.domain,
			},
		},
	})
}

func (s *Server) handleClientMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, s.oauthApp.Config.ClientMetadata())
}

// handleOAuthLogin starts the one-time org credential bootstrap: an org admin
// opens this to authorize the home server as the org.
func (s *Server) handleOAuthLogin(w http.ResponseWriter, r *http.Request) {
	url, err := s.oauthApp.StartAuthFlow(r.Context(), s.orgHandle)
	if err != nil {
		http.Error(w, "start auth flow: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
}

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	sess, err := s.oauthApp.ProcessCallback(r.Context(), r.URL.Query())
	if err != nil {
		http.Error(w, "process callback: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.store.SaveOrgSession(r.Context(), sess.AccountDID, sess.SessionID); err != nil {
		http.Error(w, "save org session: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.sap.AddManagedOrg(r.Context(), sess.AccountDID, sess.SessionID); err != nil {
		http.Error(w, "add managed org: "+err.Error(), http.StatusInternalServerError)
		return
	}
	slog.InfoContext(r.Context(), "home server authorized for org", "did", sess.AccountDID)
	_, _ = w.Write([]byte("Home server authorized. You can close this tab."))
}

func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.authCaller(w, r)
	if !ok {
		return
	}
	out, err := s.groups.ListGroups(r.Context(), caller)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, r, out)
}

func (s *Server) handleGetGroup(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.authCaller(w, r)
	if !ok {
		return
	}
	out, err := s.groups.GetGroup(r.Context(), caller, r.URL.Query().Get("group"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, r, out)
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.authCaller(w, r)
	if !ok {
		return
	}
	var in habitat.NetworkHabitatGroupsCreateGroupInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	out, err := s.groups.CreateGroup(r.Context(), caller, in)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, r, out)
}

func (s *Server) handleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.authCaller(w, r)
	if !ok {
		return
	}
	var in habitat.NetworkHabitatGroupsUpdateGroupInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	out, err := s.groups.UpdateGroup(r.Context(), caller, in)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, r, out)
}

func (s *Server) handleAddMember(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.authCaller(w, r)
	if !ok {
		return
	}
	var in habitat.NetworkHabitatGroupsAddMemberInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	out, err := s.groups.AddMember(r.Context(), caller, in)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, r, out)
}

func (s *Server) handleDeleteMember(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.authCaller(w, r)
	if !ok {
		return
	}
	var in habitat.NetworkHabitatGroupsDeleteMemberInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if err := s.groups.DeleteMember(r.Context(), caller, in); err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, r, map[string]any{})
}

// authCaller verifies the forwarded service-auth JWT pear signed on the
// caller's behalf and returns the caller (issuer) DID.
func (s *Server) authCaller(w http.ResponseWriter, r *http.Request) (syntax.DID, bool) {
	info, ok := s.serviceAuth.Validate(w, r)
	if !ok {
		return "", false
	}
	return info.Subject, true
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrGroupNotFound):
		writeXRPCError(w, http.StatusNotFound, "GroupNotFound", err.Error())
	case errors.Is(err, ErrForbidden):
		writeXRPCError(w, http.StatusForbidden, "Forbidden", err.Error())
	case errors.Is(err, ErrInvalidSubject):
		writeXRPCError(w, http.StatusBadRequest, "InvalidSubject", err.Error())
	case errors.Is(err, ErrMemberNotFound):
		writeXRPCError(w, http.StatusNotFound, "MemberNotFound", err.Error())
	case errors.Is(err, ErrNotAuthorized):
		writeXRPCError(w, http.StatusServiceUnavailable, "NotAuthorized", err.Error())
	default:
		slog.ErrorContext(r.Context(), "groups endpoint error", "err", err)
		writeXRPCError(w, http.StatusInternalServerError, "InternalServerError", err.Error())
	}
}

func writeJSON(w http.ResponseWriter, r *http.Request, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.ErrorContext(r.Context(), "encode response", "err", err)
	}
}

func writeXRPCError(w http.ResponseWriter, status int, name, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": name, "message": message})
}
