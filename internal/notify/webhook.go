package notify

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/comatproto"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Methods of the notification endpoint a syncer registers through
// com.atproto.space.authorizeSpaceSync.
const (
	methodNotifySpaceUpdate   = "com.atproto.space.notifySpaceUpdate"
	methodConfirmRegistration = "com.atproto.space.confirmRegistration"
)

// ErrRegistrationNotConfirmed reports that an endpoint did not echo the
// challenge of confirmRegistration, so it must not be registered.
var ErrRegistrationNotConfirmed = errors.New("registration not confirmed")

// WithWebhookClient sets the client webhook notifications are delivered with.
// The default only connects to public internet addresses, since endpoints are
// supplied by whoever calls authorizeSpaceSync.
func (d *Deliverer) WithWebhookClient(c *http.Client) *Deliverer {
	d.webhookClient = c
	return d
}

// NormalizeEndpoint returns endpoint without a trailing slash, the form
// registrations are stored and delivered under.
func NormalizeEndpoint(endpoint string) string {
	return strings.TrimRight(endpoint, "/")
}

// ConfirmRegistration asks endpoint to confirm it issued token for space, as
// the space host must before registering it: it POSTs confirmRegistration with
// a fresh challenge and requires the endpoint to echo it back.
func ConfirmRegistration(
	ctx context.Context,
	client *http.Client,
	space habitat_syntax.SpaceURI,
	endpoint string,
	token string,
) error {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Errorf("generate challenge: %w", err)
	}
	challenge := hex.EncodeToString(raw[:])
	body, err := json.Marshal(map[string]string{
		"space": space.String(), "token": token, "challenge": challenge,
	})
	if err != nil {
		return fmt.Errorf("marshal confirmRegistration: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, deliveryTimeout)
	defer cancel()
	resp, err := postJSON(ctx, client, endpoint, methodConfirmRegistration, body)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRegistrationNotConfirmed, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("%w: status %d", ErrRegistrationNotConfirmed, resp.StatusCode)
	}
	var out struct {
		Challenge string `json:"challenge"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&out); err != nil {
		return fmt.Errorf("%w: decode response: %w", ErrRegistrationNotConfirmed, err)
	}
	if out.Challenge != challenge {
		return fmt.Errorf("%w: wrong challenge", ErrRegistrationNotConfirmed)
	}
	return nil
}

func postJSON(
	ctx context.Context,
	client *http.Client,
	endpoint string,
	method string,
	body []byte,
) (*http.Response, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, endpoint+"/xrpc/"+method, bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return client.Do(req)
}

// notifyWebhooks delivers update to every endpoint registered for space
// through authorizeSpaceSync, in the background and best-effort. A registration
// whose credential has expired is sent registrationExpired in place of its
// first update after expiry and nothing after, unless includeLapsed is set,
// which a deletion notice is: it reaches lapsed registrations too.
func (d *Deliverer) notifyWebhooks(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	spaceRev syntax.TID,
	update any,
	includeLapsed bool,
) {
	hooks, err := d.store.ListWebhooks(ctx, space)
	if err != nil {
		slog.ErrorContext(ctx, "notify: list webhooks", "err", err, "space", space)
		return
	}
	// Delivery outlives the originating request, so drop ctx's cancellation
	// while keeping its trace context.
	deliverCtx := context.WithoutCancel(ctx)
	for _, h := range hooks {
		payload := update
		expired := !d.now().Before(h.ExpiresAt)
		if expired && !includeLapsed {
			if h.ExpiryNotified {
				continue
			}
			payload = comatproto.ComAtprotoSpaceNotifySpaceUpdateRegistrationExpired{
				SpaceRev: spaceRev.String(),
			}
		}
		body, err := json.Marshal(comatproto.ComAtprotoSpaceNotifySpaceUpdateInput{
			Space: space.AtSpace().String(), Token: h.Token, Update: payload,
		})
		if err != nil {
			slog.ErrorContext(ctx, "notify: marshal notifySpaceUpdate", "err", err)
			return
		}
		sendExpiry := expired && !includeLapsed
		go func() {
			if err := d.sendWebhook(deliverCtx, h, body); err != nil {
				slog.WarnContext(deliverCtx, "notify: deliver notifySpaceUpdate",
					"err", err, "endpoint", h.Endpoint)
				return
			}
			if sendExpiry {
				if err := d.store.MarkWebhookExpiryNotified(deliverCtx, h); err != nil {
					slog.ErrorContext(deliverCtx, "notify: mark expiry notified", "err", err)
				}
			}
		}()
	}
}

func (d *Deliverer) sendWebhook(ctx context.Context, h Webhook, body []byte) error {
	ctx, cancel := context.WithTimeout(ctx, deliveryTimeout)
	defer cancel()
	resp, err := postJSON(ctx, d.webhookClient, h.Endpoint, methodNotifySpaceUpdate, body)
	if err != nil {
		return fmt.Errorf("deliver to %s: %w", h.Endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("delivery to %s rejected: status %d", h.Endpoint, resp.StatusCode)
	}
	return nil
}

// NotifyWriterRemoved tells registered endpoints a repo was removed from the
// space's writer set, with the revisions the removal was sequenced at.
func (d *Deliverer) NotifyWriterRemoved(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	spaceRev syntax.TID,
	prevSpaceRev syntax.TID,
) {
	d.notifyWebhooks(ctx, space, spaceRev, comatproto.ComAtprotoSpaceDefsRepoRemoved{
		Repo: repo.String(), SpaceRev: spaceRev.String(), PrevSpaceRev: prevSpaceRev.String(),
	}, false)
}

// repoWriteUpdate is the notifySpaceUpdate payload for a write.
func repoWriteUpdate(
	repo syntax.DID,
	rev syntax.TID,
	hash []byte,
	spaceRev syntax.TID,
	prevSpaceRev syntax.TID,
) comatproto.ComAtprotoSpaceDefsRepoWrite {
	return comatproto.ComAtprotoSpaceDefsRepoWrite{
		Repo:         repo.String(),
		RepoRev:      rev.String(),
		Data:         atdata.Bytes(hash),
		SpaceRev:     spaceRev.String(),
		PrevSpaceRev: prevSpaceRev.String(),
	}
}

// spaceDeletedUpdate is the notifySpaceUpdate payload for a deleted space.
func spaceDeletedUpdate() comatproto.ComAtprotoSpaceNotifySpaceUpdateSpaceDeleted {
	return comatproto.ComAtprotoSpaceNotifySpaceUpdateSpaceDeleted{}
}
