package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/syntax"
	habitatapi "github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/demos/fruitgang/internal/index"
	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/habitat-network/habitat/internal/sap"
)

type fruitgangServer struct {
	sap         *sap.Sap
	oauthApp    *oauthclient.App
	store       *index.Store
	frontendURL string
}

func newFruitgangServer(sapInstance *sap.Sap, oauthApp *oauthclient.App, store *index.Store, frontendURL string) *fruitgangServer {
	return &fruitgangServer{sap: sapInstance, oauthApp: oauthApp, store: store, frontendURL: frontendURL}
}

func (s *fruitgangServer) handleClientMetadata(w http.ResponseWriter, _ *http.Request) {
	cm := s.oauthApp.Config.ClientMetadata()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(cm); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleAddOrg connects the given org handle to Fruit Gang.
// The admin types their org's handle; if sap already holds a live session for
// that org (e.g. a prior connection attempt got the OAuth login done but
// failed to create the space or persist it), we retry using that session
// instead of forcing the admin through the OAuth dance again. Otherwise the
// backend resolves the handle and starts a fresh OAuth flow.
func (s *fruitgangServer) handleAddOrg(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w, r)

	var req struct {
		Handle string `json:"handle"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Handle == "" {
		http.Error(w, "handle required", http.StatusBadRequest)
		return
	}

	atid, err := syntax.ParseAtIdentifier(req.Handle)
	if err != nil {
		http.Error(w, "invalid identifier", http.StatusBadRequest)
		return
	}

	if did, err := s.oauthApp.ResolveDID(r.Context(), atid.String()); err == nil {
		if s.tryReconnectWithExistingSession(r.Context(), w, did) {
			return
		}
	}

	redirectURL, err := s.oauthApp.StartAuthFlow(r.Context(), atid.String())
	if err != nil {
		http.Error(w, fmt.Sprintf("start auth flow: %s", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(map[string]string{"redirect_url": redirectURL})
}

// tryReconnectWithExistingSession attempts to finish connecting the org using
// a session sap already holds for it, so a failure in the space-creation step
// doesn't force the admin back through a full OAuth login just to retry it.
// It returns true if it fully handled the request (wrote either a success or
// an error response); false means sap has no usable session for this org and
// the caller should fall back to starting a fresh OAuth flow.
func (s *fruitgangServer) tryReconnectWithExistingSession(ctx context.Context, w http.ResponseWriter, did syntax.DID) bool {
	client, err := s.sap.GetClient(ctx, did)
	if err != nil {
		return false
	}

	spaceURI, err := s.connectOrgSpace(ctx, client, did)
	if err != nil {
		slog.WarnContext(ctx, "retry with existing session failed, falling back to fresh oauth flow", "did", did, "err", err)
		return false
	}

	slog.InfoContext(ctx, "org reconnected using existing session", "did", did, "space", spaceURI)
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(map[string]string{"redirect_url": s.frontendURL})
	return true
}

func (s *fruitgangServer) handleAddOrgCORSPreflight(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *fruitgangServer) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	sessionData, err := s.oauthApp.ProcessCallback(r.Context(), r.URL.Query())
	if err != nil {
		http.Error(w, fmt.Sprintf("process callback: %s", err), http.StatusInternalServerError)
		return
	}

	if err := s.sap.AddManagedOrg(r.Context(), sessionData.AccountDID, sessionData.SessionID); err != nil {
		http.Error(w, fmt.Sprintf("save org: %s", err), http.StatusInternalServerError)
		return
	}

	client, err := s.oauthApp.GetClient(r.Context(), sessionData.AccountDID, sessionData.SessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("get oauth client: %s", err), http.StatusInternalServerError)
		return
	}

	spaceURI, err := s.connectOrgSpace(r.Context(), client, sessionData.AccountDID)
	if err != nil {
		slog.ErrorContext(r.Context(), "connect org space failed", "did", sessionData.AccountDID, "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	slog.InfoContext(r.Context(), "org onboarded", "did", sessionData.AccountDID, "space", spaceURI)
	http.Redirect(w, r, s.frontendURL, http.StatusSeeOther)
}

// connectOrgSpace creates (or finds) the org's fruitgang community space,
// grants member access, and persists it as the org's default space -- the
// flag the frontend checks to know the org is connected.
func (s *fruitgangServer) connectOrgSpace(ctx context.Context, client *http.Client, did syntax.DID) (string, error) {
	spaceURI, err := s.ensureSpace(ctx, client, did)
	if err != nil {
		return "", fmt.Errorf("ensure space: %w", err)
	}

	if err := s.store.SetDefaultSpace(did.String(), spaceURI); err != nil {
		return "", fmt.Errorf("save space: %w", err)
	}

	return spaceURI, nil
}

// ensureSpace creates the fruitgang community space for the org (or finds the existing one),
// then grants write access to all current org members.
func (s *fruitgangServer) ensureSpace(ctx context.Context, client *http.Client, did syntax.DID) (string, error) {
	input := habitatapi.NetworkHabitatSpaceCreateSpaceInput{
		Type: "network.habitat.group",
		Skey: "fruitgang",
	}
	body, err := json.Marshal(input)
	if err != nil {
		return "", err
	}

	resp, err := client.Post("/xrpc/network.habitat.space.createSpace", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create space request: %w", err)
	}
	defer resp.Body.Close()

	var spaceURI string
	if resp.StatusCode == http.StatusOK {
		var out habitatapi.NetworkHabitatSpaceCreateSpaceOutput
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return "", fmt.Errorf("decode createSpace response: %w", err)
		}
		spaceURI = out.Uri
	} else if resp.StatusCode == http.StatusConflict {
		spaceURI, err = s.findExistingSpace(ctx, client, did)
		if err != nil {
			return "", err
		}
	} else {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("createSpace returned %d: %s", resp.StatusCode, string(respBody))
	}

	if err := s.grantOrgAccess(ctx, client, spaceURI); err != nil {
		slog.WarnContext(ctx, "some members could not be granted space access", "err", err)
	}
	return spaceURI, nil
}

// grantOrgAccess grants roles on the space to all current org members and admins via
// the relationship API. Admins receive manager role; non-admin members receive writer role.
func (s *fruitgangServer) grantOrgAccess(ctx context.Context, client *http.Client, spaceURI string) error {
	adminsResp, err := client.Get("/xrpc/network.habitat.org.getAdmins")
	if err != nil {
		return fmt.Errorf("getAdmins request: %w", err)
	}
	defer adminsResp.Body.Close()
	if adminsResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(adminsResp.Body)
		return fmt.Errorf("getAdmins returned %d: %s", adminsResp.StatusCode, string(body))
	}
	var adminsOut habitatapi.NetworkHabitatOrgGetAdminsOutput
	if err := json.NewDecoder(adminsResp.Body).Decode(&adminsOut); err != nil {
		return fmt.Errorf("decode getAdmins response: %w", err)
	}

	membersResp, err := client.Get("/xrpc/network.habitat.org.getMembers")
	if err != nil {
		return fmt.Errorf("getMembers request: %w", err)
	}
	defer membersResp.Body.Close()
	if membersResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(membersResp.Body)
		return fmt.Errorf("getMembers returned %d: %s", membersResp.StatusCode, string(body))
	}
	var membersOut habitatapi.NetworkHabitatOrgGetMembersOutput
	if err := json.NewDecoder(membersResp.Body).Decode(&membersOut); err != nil {
		return fmt.Errorf("decode getMembers response: %w", err)
	}

	adminSet := make(map[string]bool, len(adminsOut.Admins))
	for _, a := range adminsOut.Admins {
		adminSet[a.Did] = true
	}

	for _, a := range adminsOut.Admins {
		if err := s.writeTuple(ctx, client, spaceURI, a.Did, "manager"); err != nil {
			slog.WarnContext(ctx, "could not grant admin manager role", "did", a.Did, "err", err)
		}
	}
	for _, m := range membersOut.Members {
		if adminSet[m.Did] {
			continue
		}
		if err := s.writeTuple(ctx, client, spaceURI, m.Did, "writer"); err != nil {
			slog.WarnContext(ctx, "could not grant member writer role", "did", m.Did, "err", err)
		}
	}
	return nil
}

func (s *fruitgangServer) writeTuple(_ context.Context, client *http.Client, spaceURI, did, relation string) error {
	input := habitatapi.NetworkHabitatRelationshipWriteTupleInput{
		Subject: map[string]any{
			"$type": "network.habitat.relationship.defs#userSubject",
			"did":   did,
		},
		Relation: relation,
		Object:   habitatapi.NetworkHabitatRelationshipDefsSpaceObject{Space: spaceURI},
	}
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	resp, err := client.Post("/xrpc/network.habitat.relationship.writeTuple", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("writeTuple request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("writeTuple returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (s *fruitgangServer) findExistingSpace(_ context.Context, client *http.Client, did syntax.DID) (string, error) {
	params := url.Values{}
	params.Set("type", "network.habitat.group")
	params.Set("did", did.String())

	resp, err := client.Get("/xrpc/network.habitat.space.listSpaces?" + params.Encode())
	if err != nil {
		return "", fmt.Errorf("list spaces request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("listSpaces returned %d: %s", resp.StatusCode, string(respBody))
	}

	var out habitatapi.NetworkHabitatSpaceListSpacesOutput
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode listSpaces response: %w", err)
	}

	for _, sp := range out.Spaces {
		if sp.Skey == "fruitgang" {
			return sp.Uri, nil
		}
	}

	if len(out.Spaces) > 0 {
		return out.Spaces[0].Uri, nil
	}

	return "", fmt.Errorf("no network.habitat.group space found for %s", did)
}

func setCORSHeaders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
}
