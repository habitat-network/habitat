package pearserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// registrationTTL is how long a registerNotify subscription stays valid before
// the syncer must renew it.
const registrationTTL = 24 * time.Hour

// RegisterNotify handles network.habitat.space.registerNotify: a syncer
// authenticated with a space credential subscribes a service to notifyWrite
// events for the whole space or a specific repo.
//
// The subscriber normally names itself with a service identifier — a DID with
// an optional service fragment — which is both how deliveries are addressed
// (the service-auth audience) and how the delivery endpoint is found, by
// resolving the DID document.
//
// The `endpoint` field predates that and is kept for compatibility: a
// subscriber may still pass a bare URL, which then serves as both the delivery
// address and the audience. `service` wins if both are given.
func (p *PearServer) RegisterNotify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSpaceRegisterNotifyInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	// The space credential must authorize the space being registered against.
	if _, ok = p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodSpaceCredential),
		authn.WithSpace(spaceURI, habitat_syntax.SpaceRoleReader),
	).Validate(w, r); !ok {
		return
	}
	var repo syntax.DID
	if input.Repo != "" {
		repo, ok = httpx.ParseDIDInput(ctx, w, input.Repo, "repo")
		if !ok {
			return
		}
	}
	service, endpoint, ok := p.resolveNotifyTarget(ctx, w, input)
	if !ok {
		return
	}
	expiresAt := time.Now().Add(registrationTTL)
	if err := p.notifyStore.Register(
		ctx, spaceURI, repo, endpoint, service, expiresAt,
	); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("register notify: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSpaceRegisterNotifyOutput{
		ExpiresAt: expiresAt.UTC().Format(time.RFC3339),
	})
}

// resolveNotifyTarget works out which service a registration subscribes and
// where its notifications get delivered, writing the error response itself
// when neither can be determined. It returns service as the identifier to
// address notifications to, or empty for a registration made through the
// deprecated endpoint field.
func (p *PearServer) resolveNotifyTarget(
	ctx context.Context,
	w http.ResponseWriter,
	input habitat.NetworkHabitatSpaceRegisterNotifyInput,
) (service, endpoint string, ok bool) {
	if input.Service == "" {
		if input.Endpoint == "" {
			httpx.WriteInvalidRequest(
				ctx, w, "one of service or endpoint is required", nil,
			)
			return "", "", false
		}
		// Deprecated path: the caller supplied the delivery address outright,
		// so it is also what deliveries are addressed to.
		return "", input.Endpoint, true
	}

	did, serviceID, ok := httpx.ParseServiceRefInput(
		ctx, w, input.Service, "service identifier",
	)
	if !ok {
		return "", "", false
	}
	// Use context.WithoutCancel to avoid cached context cancelled errors:
	// https://github.com/bluesky-social/indigo/pull/1345
	ident, err := p.dir.LookupDID(context.WithoutCancel(ctx), did)
	if err != nil {
		writeServiceNotResolvable(ctx, w, input.Service, fmt.Errorf("resolve %s: %w", did, err))
		return "", "", false
	}
	resolved := ident.GetServiceEndpoint(serviceID)
	if resolved == "" {
		writeServiceNotResolvable(
			ctx, w, input.Service,
			fmt.Errorf("%s publishes no %q service", did, serviceID),
		)
		return "", "", false
	}
	// Both halves are persisted: the endpoint to deliver to, and the
	// subscriber's own service string rather than what it resolved to, so the
	// registration keeps addressing the subscriber the same way for its whole
	// lifetime even if its DID document later changes. The store normalizes
	// the endpoint's trailing slash.
	return input.Service, resolved, true
}

// writeServiceNotResolvable rejects a service identifier that has no endpoint to
// deliver to. The underlying cause is logged but kept out of the response,
// since resolver errors can name internal endpoints.
func writeServiceNotResolvable(
	ctx context.Context, w http.ResponseWriter, service string, err error,
) {
	slog.WarnContext(ctx, "register notify: service not resolvable",
		"service", service, "err", err)
	httpx.WriteError(
		ctx, w, "ServiceNotResolvable",
		"could not resolve the service identifier to a service endpoint",
		http.StatusBadRequest,
	)
}
