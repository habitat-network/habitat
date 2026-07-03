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
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"

	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/habitat-network/habitat/internal/sap"
)

type fruitgangServer struct {
	sap         *sap.Sap
	oauthApp    *oauthclient.App
	frontendURL string
}

func newFruitgangServer(sapInstance *sap.Sap, oauthApp *oauthclient.App, frontendURL string) *fruitgangServer {
	return &fruitgangServer{sap: sapInstance, oauthApp: oauthApp, frontendURL: frontendURL}
}

func (s *fruitgangServer) handleClientMetadata(w http.ResponseWriter, _ *http.Request) {
	cm := s.oauthApp.Config.ClientMetadata()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(cm); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleAddOrg initiates the OAuth flow for the given org handle.
// The admin types their org's handle; the backend resolves it and starts the OAuth dance.
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

func (s *fruitgangServer) handleAddOrgCORSPreflight(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// handleOAuthCallback finishes connecting the org: it creates (or finds) the
// org's Fruit Gang space and grants every org member access to it, and only
// then tells sap to start managing the org. That ordering matters: sap
// managing an org is the single durable signal /getSpaceURI relies on to
// decide the org is connected (see internal/server/server.go), so a failure
// while setting up the space must not leave sap thinking the org is ready --
// the admin can simply retry the OAuth flow from scratch, since nothing
// partial was persisted.
func (s *fruitgangServer) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	sessionData, err := s.oauthApp.ProcessCallback(r.Context(), r.URL.Query())
	if err != nil {
		http.Error(w, fmt.Sprintf("process callback: %s", err), http.StatusInternalServerError)
		return
	}

	client, err := s.oauthApp.GetClient(r.Context(), sessionData.AccountDID, sessionData.SessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("get oauth client: %s", err), http.StatusInternalServerError)
		return
	}

	if err := s.connectOrgSpace(r.Context(), client, sessionData.AccountDID); err != nil {
		slog.ErrorContext(r.Context(), "connect org space failed", "did", sessionData.AccountDID, "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := s.sap.AddManagedOrg(r.Context(), sessionData.AccountDID, sessionData.SessionID); err != nil {
		http.Error(w, fmt.Sprintf("save org: %s", err), http.StatusInternalServerError)
		return
	}

	slog.InfoContext(r.Context(), "org onboarded", "did", sessionData.AccountDID)
	http.Redirect(w, r, s.frontendURL, http.StatusSeeOther)
}

// connectOrgSpace creates (or finds) the org's Fruit Gang space and grants
// every org member write access to it.
func (s *fruitgangServer) connectOrgSpace(ctx context.Context, client *http.Client, did syntax.DID) error {
	spaceURI, err := s.ensureSpace(ctx, client, did)
	if err != nil {
		return fmt.Errorf("ensure space: %w", err)
	}

	if err := s.grantOrgMembersAccess(client, did, spaceURI); err != nil {
		return fmt.Errorf("grant org access: %w", err)
	}

	return nil
}

// ensureSpace creates the fruitgang community space for the org, or finds the
// existing one if it was already created by a prior connection attempt.
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

	switch resp.StatusCode {
	case http.StatusOK:
		var out habitatapi.NetworkHabitatSpaceCreateSpaceOutput
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return "", fmt.Errorf("decode createSpace response: %w", err)
		}
		return out.Uri, nil
	case http.StatusConflict:
		return s.findExistingSpace(ctx, client, did)
	default:
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("createSpace returned %d: %s", resp.StatusCode, string(respBody))
	}
}

// grantOrgMembersAccess grants writer access on spaceURI to every current and
// future member of the org, in a single idempotent call. It does this by
// referencing the org's self space (ats://<org>/network.habitat.organization/self)
// as the grant's subject: every org member automatically holds "reader" on
// that space (see fgastore.OrgMemberContextualTuple), so granting "writer" on
// spaceURI to "readers of the org's self space" covers the whole org without
// enumerating members or admins individually.
func (s *fruitgangServer) grantOrgMembersAccess(client *http.Client, did syntax.DID, spaceURI string) error {
	orgSelfSpace := habitat_syntax.ConstructSpaceURI(did, "network.habitat.organization", "self")

	input := habitatapi.NetworkHabitatRelationshipWriteTupleInput{
		Subject: map[string]any{
			"$type": "network.habitat.relationship.defs#spaceRoleSubject",
			"space": orgSelfSpace.String(),
			"role":  "reader",
		},
		Relation: "writer",
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
