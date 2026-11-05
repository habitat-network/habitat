package pearserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/bluesky-social/indigo/atproto/identity"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/clientmetadata"
	"github.com/habitat-network/habitat/internal/httpsig"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
)

func (p *PearServer) GetSpaceCredential(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSpaceGetSpaceCredentialInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	// A deleted space answers SpaceDeleted before any authorization, so a
	// syncer that missed the notifySpaceDeleted learns of it when it renews:
	// the deletion revoked every member, so it would otherwise only learn that
	// it is no longer one.
	deleted, err := p.spacesStore.IsSpaceDeleted(ctx, spaceURI)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("check space deleted: %w", err))
		return
	} else if deleted {
		httpx.WriteError(ctx, w, "SpaceDeleted", "space was deleted", http.StatusBadRequest)
		return
	}
	// The credential is bound to credentialKid: every request presenting it
	// must be signed by that key.
	if !httpsig.IsP256DIDKey(input.CredentialKid) {
		httpx.WriteInvalidRequest(
			ctx, w, "credentialKid must be a P-256 did:key",
			fmt.Errorf("bad credentialKid %q", input.CredentialKid),
		)
		return
	}
	policy, err := p.spacesStore.GetSpacePolicy(ctx, spaceURI)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("get space policy: %w", err))
		return
	}
	// Only a memberspace with publicRead issues credentials without a user:
	// every other space gates on the user, so a request without a delegation
	// token answers SpaceNotFound, which doesn't reveal whether the space
	// exists.
	publicRead := policy.Type == spaces.PolicyMemberSpace && policy.PublicRead
	if input.DelegationToken == "" && !publicRead {
		httpx.WriteSpaceNotFound(ctx, w, errors.New("delegation token required"))
		return
	}
	if input.DelegationToken != "" {
		if _, err := p.delegations.Verify(ctx, input.DelegationToken, spaceURI); err != nil {
			switch {
			case errors.Is(err, authn.ErrInvalidDelegationToken):
				httpx.WriteError(
					ctx, w, "InvalidDelegationToken", err.Error(), http.StatusBadRequest,
				)
			case errors.Is(err, authn.ErrUserNotAuthorized):
				httpx.WriteError(ctx, w, "UserNotAuthorized", "", http.StatusBadRequest)
			default:
				httpx.WriteServerError(ctx, w, err)
			}
			return
		}
	}
	if !p.verifyClientAttestation(ctx, w, spaceURI, policy, input.ClientAttestation) {
		return
	}
	privKey, err := p.hive.PrivateKeyForDID(ctx, spaceURI.SpaceOwner())
	if errors.Is(err, identity.ErrDIDNotFound) {
		// Spaces owned by the pear host itself are signed with the host key,
		// which the host's DID document publishes as #atproto.
		privKey = p.hostKey
	} else if err != nil {
		httpx.WriteSpaceNotFound(ctx, w, fmt.Errorf("failed to get host private key: %w", err))
		return
	}
	token, err := utils.SpaceCredential(
		privKey, spaceURI, utils.WithConfirmationKey(input.CredentialKid),
	)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("failed to sign token: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSpaceGetSpaceCredentialOutput{Credential: token})
}

func (p *PearServer) verifyClientAttestation(
	ctx context.Context,
	w http.ResponseWriter,
	spaceURI habitat_syntax.SpaceURI,
	policy spaces.Policy,
	attestation string,
) bool {
	// A memberspace allowedApps list limits which apps may read, checked
	// against the attested client_id, so a credential request must attest.
	if policy.Type == spaces.PolicyMemberSpace && len(policy.AllowedApps) > 0 {
		if attestation == "" {
			httpx.WriteError(
				ctx, w, "AppNotAuthorized", "space restricts readers to allowed apps",
				http.StatusBadRequest,
			)
			return false
		}
		clientID, err := clientmetadata.VerifyAttestation(
			ctx, p.clientMeta, attestation, spaceURI.SpaceOwner(),
		)
		if errors.Is(err, clientmetadata.ErrInvalidAttestation) {
			httpx.WriteInvalidClientAttestation(ctx, w, err.Error(), err)
			return false
		} else if err != nil {
			httpx.WriteServerError(ctx, w, fmt.Errorf("verify attestation: %w", err))
			return false
		}
		if !slices.Contains(policy.AllowedApps, clientID) {
			httpx.WriteError(
				ctx, w, "AppNotAuthorized", "app is not in the space's allowedApps",
				http.StatusBadRequest,
			)
			return false
		}
	}
	orgDID := spaceURI.SpaceOwner()
	isOrg, err := p.opensocialStore.IsOrg(ctx, orgDID)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("failed to check if org: %w", err))
		return false
	}
	if !isOrg {
		return true
	}
	if attestation == "" {
		// Per https://atproto.com/specs/oauth, a public client has no signing
		// key, so it structurally cannot produce a client attestation (which
		// must be signed by the client's own key) — there's no client_id to
		// check app access against. Only confidential clients (which can
		// attest) are gated by the org's app-access allow-list.
		return true
	}
	clientID, err := clientmetadata.VerifyAttestation(ctx, p.clientMeta, attestation, orgDID)
	if errors.Is(err, clientmetadata.ErrInvalidAttestation) {
		httpx.WriteInvalidClientAttestation(ctx, w, err.Error(), err)
		return false
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("verify attestation: %w", err))
		return false
	}
	granted, err := p.opensocialStore.CheckAppAccess(ctx, spaceURI, clientID)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("check app access grant: %w", err))
		return false
	}
	if !granted {
		httpx.WriteError(
			ctx, w, "AppNotAuthorized", "org has not approved app",
			http.StatusBadRequest,
		)
		return false
	}
	return true
}
