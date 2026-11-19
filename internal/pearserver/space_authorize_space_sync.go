package pearserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/habitat-network/habitat/api/comatproto"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/notify"
	"github.com/habitat-network/habitat/internal/spaces"
	"github.com/habitat-network/habitat/internal/utils"
)

// maxNotifyTokenLen bounds the opaque token a syncer registers, which is
// stored per registration and echoed in every notification.
const maxNotifyTokenLen = 512

// AuthorizeSpaceSync handles com.atproto.space.authorizeSpaceSync: it issues a
// space credential, as getSpaceCredential does, and optionally registers a
// webhook to be told about changes to the space for as long as the credential
// is valid. Before registering, the host calls the endpoint's
// confirmRegistration, so a caller can't point notifications at a service that
// didn't ask for them.
//
// The endpoint takes no authentication, and the host makes an outbound request
// to a caller-supplied URL, so the endpoint must be https and the request goes
// through a client that refuses non-public addresses.
func (p *PearServer) AuthorizeSpaceSync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input comatproto.ComAtprotoSpaceAuthorizeSpaceSyncInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	if input.Notify != nil {
		if err := p.validateNotify(*input.Notify); err != nil {
			httpx.WriteInvalidRequest(ctx, w, "invalid notify", err)
			return
		}
	}
	token, ok := p.issueCredential(ctx, w, spaceURI, credentialRequest{
		credentialKey:     input.CredentialKey,
		delegationToken:   input.DelegationToken,
		clientAttestation: input.ClientAttestation,
		userNotAuthorized: "AccountNotAuthorized",
	})
	if !ok {
		return
	}
	if input.Notify != nil {
		endpoint := notify.NormalizeEndpoint(input.Notify.Endpoint)
		err := notify.ConfirmRegistration(
			ctx, p.webhookClient, spaceURI, endpoint, input.Notify.Token,
		)
		if errors.Is(err, notify.ErrRegistrationNotConfirmed) {
			httpx.WriteError(
				ctx, w, "RegistrationNotConfirmed", "endpoint did not confirm the registration",
				http.StatusBadRequest,
			)
			return
		} else if err != nil {
			httpx.WriteServerError(ctx, w, fmt.Errorf("confirm registration: %w", err))
			return
		}
		// The registration lives as long as the credential it came with.
		expiresAt := time.Now().Add(utils.DefaultSpaceCredentialTTL)
		if err := p.notifyStore.RegisterWebhook(
			ctx, spaceURI, endpoint, input.Notify.Token, expiresAt,
		); err != nil {
			httpx.WriteServerError(ctx, w, fmt.Errorf("register webhook: %w", err))
			return
		}
	}
	out := comatproto.ComAtprotoSpaceAuthorizeSpaceSyncOutput{Credential: token}
	// The revision is read after confirming and registering, so a write that
	// lands in between is covered by the notification rather than missed.
	rev, err := p.spacesStore.SpaceRev(ctx, spaceURI)
	if err != nil && !errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteServerError(ctx, w, fmt.Errorf("get space rev: %w", err))
		return
	}
	out.SpaceRev = rev.String()
	httpx.WriteJSON(ctx, w, out)
}

// validateNotify checks the webhook a caller asked to register.
func (p *PearServer) validateNotify(n comatproto.ComAtprotoSpaceAuthorizeSpaceSyncNotify) error {
	if n.Token == "" || len(n.Token) > maxNotifyTokenLen {
		return fmt.Errorf("token must be 1 to %d characters", maxNotifyTokenLen)
	}
	u, err := url.Parse(n.Endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("endpoint must be an absolute URL without credentials or query")
	}
	if u.Scheme != "https" && !(p.insecureWebhooks && u.Scheme == "http") {
		return errors.New("endpoint must be https")
	}
	return nil
}
