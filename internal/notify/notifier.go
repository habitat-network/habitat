package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/golang-jwt/jwt/v5"
	"github.com/habitat-network/habitat/api/habitat"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
)

var (
	nsidNotifyWrite        = syntax.NSID("network.habitat.space.notifyWrite")
	nsidNotifySpaceDeleted = syntax.NSID("network.habitat.space.notifySpaceDeleted")
)

// ServiceAuthSigner mints a habitat-issued atproto service-auth JWT for the
// issuing identity. hive.Hive satisfies this interface.
type ServiceAuthSigner interface {
	SignJWT(
		ctx context.Context,
		did syntax.DID,
		headers map[string]any,
		claims jwt.Claims,
	) (string, error)
}

// Deliverer delivers notifyWrite / notifySpaceDeleted events to registered
// syncer endpoints. It satisfies the spaces.Notifier interface.
type Deliverer struct {
	store  Store
	client *http.Client
	signer ServiceAuthSigner
}

func NewNotifier(store Store, client *http.Client, signer ServiceAuthSigner) *Deliverer {
	return &Deliverer{store: store, client: client, signer: signer}
}

// NotifyWrite looks up the registrations that subscribe to a write on repo
// within space and delivers a notifyWrite to each endpoint in the background,
// best-effort. Delivery is decoupled from ctx's cancellation so it survives the
// originating request.
func (d *Deliverer) NotifyWrite(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	rev syntax.TID,
) {
	regs, err := d.store.ListForRepo(ctx, space, repo)
	if err != nil {
		slog.ErrorContext(ctx, "notify: list registrations",
			"err", err, "space", space, "repo", repo)
		return
	}
	if len(regs) == 0 {
		return
	}

	body, err := json.Marshal(habitat.NetworkHabitatSpaceNotifyWriteInput{
		Space: space.String(),
		Repo:  repo.String(),
		Rev:   rev.String(),
		Hash:  "",
	})
	if err != nil {
		slog.ErrorContext(ctx, "notify: marshal notifyWrite", "err", err)
		return
	}

	d.fanout(ctx, space.SpaceOwner(), nsidNotifyWrite, regs, body)
}

// NotifySpaceDeleted delivers a notifySpaceDeleted to every endpoint registered
// for the space, best-effort, so syncers stop tracking it.
func (d *Deliverer) NotifySpaceDeleted(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) {
	regs, err := d.store.ListForSpace(ctx, space)
	if err != nil {
		slog.ErrorContext(ctx, "notify: list registrations", "err", err, "space", space)
		return
	}
	if len(regs) == 0 {
		return
	}

	body, err := json.Marshal(habitat.NetworkHabitatSpaceNotifySpaceDeletedInput{
		Space: space.String(),
	})
	if err != nil {
		slog.ErrorContext(ctx, "notify: marshal notifySpaceDeleted", "err", err)
		return
	}

	d.fanout(ctx, space.SpaceOwner(), nsidNotifySpaceDeleted, regs, body)
}

// fanout signs a per-endpoint service-auth JWT for the space authority and
// delivers body to each registration's endpoint in the background.
func (d *Deliverer) fanout(
	ctx context.Context,
	iss syntax.DID,
	method syntax.NSID,
	regs []Registration,
	body []byte,
) {
	// Delivery is best-effort and outlives the originating request, so drop
	// ctx's cancellation while keeping its trace context.
	deliverCtx := context.WithoutCancel(ctx)
	for _, reg := range regs {
		go d.deliver(deliverCtx, iss, method, reg.Endpoint, body)
	}
}

func (d *Deliverer) deliver(
	ctx context.Context,
	iss syntax.DID,
	method syntax.NSID,
	endpoint string,
	body []byte,
) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		slog.ErrorContext(ctx, "notify: build request", "err", err, "endpoint", endpoint)
		return
	}

	headers, claims := utils.ServiceAuthClaims(iss, endpoint, &method, nil)
	token, err := d.signer.SignJWT(ctx, iss, headers, claims)
	if err != nil {
		slog.ErrorContext(ctx, "notify: sign service auth",
			"err", err, "endpoint", endpoint, "method", method)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := d.client.Do(req)
	if err != nil {
		slog.WarnContext(ctx, "notify: deliver", "err", err, "endpoint", endpoint)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusMultipleChoices {
		slog.WarnContext(ctx, "notify: delivery rejected",
			"status", resp.StatusCode, "endpoint", endpoint)
	}
}
