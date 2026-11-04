package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	_ "github.com/bluesky-social/indigo/atproto/auth" // registers the ES256/ES256K signing methods jwt.GetSigningMethod resolves below

	"github.com/habitat-network/habitat/api/habitat"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
)

// Notification method names, delivered in each registration's namespace.
const (
	methodNotifyWrite        = "notifyWrite"
	methodNotifySpaceDeleted = "notifySpaceDeleted"
)

// ServiceAuthSigner mints a habitat-issued atproto service-auth JWT for the
// issuing identity. hive.Hive satisfies this interface.
type ServiceAuthSigner interface {
	PrivateKeyForDID(ctx context.Context, did syntax.DID) (atcrypto.PrivateKey, error)
}

// Deliverer delivers notifyWrite / notifySpaceDeleted events to registered
// syncer endpoints. It satisfies the spaces.Notifier interface.
type Deliverer struct {
	store  Store
	client *http.Client
	signer ServiceAuthSigner
	now    func() time.Time
}

const (
	// retryInterval is how often Run looks for due notifySpaceDeleted retries.
	retryInterval = 10 * time.Second
	// retryPageSize bounds how many retries Run attempts at once.
	retryPageSize = 20
	// deliveryTimeout bounds a single delivery attempt.
	deliveryTimeout = 10 * time.Second
	// deliveryLease is how long a claimed delivery stays claimed; it must
	// outlast deliveryTimeout so no other worker retries an attempt in flight.
	deliveryLease = time.Minute
)

func NewNotifier(store Store, client *http.Client, signer ServiceAuthSigner) *Deliverer {
	return &Deliverer{store: store, client: client, signer: signer, now: time.Now}
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
	hash []byte,
	spaceRev syntax.TID,
	prevSpaceRev syntax.TID,
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
		Space:   space.String(),
		Repo:    repo.String(),
		Rev:     rev.String(),
		Hash:    atdata.Bytes(hash),
		RepoRev: rev.String(),
		// The space host sequences writes with its own revision; sending the
		// previous one too lets a syncer see a gap and catch up.
		SpaceRev:     spaceRev.String(),
		PrevSpaceRev: prevSpaceRev.String(),
	})
	if err != nil {
		slog.ErrorContext(ctx, "notify: marshal notifyWrite", "err", err)
		return
	}

	d.fanout(ctx, space.SpaceOwner(), methodNotifyWrite, regs, body)
}

// NotifySpaceDeleted reliably delivers a notifySpaceDeleted to every syncer
// registered for the space, so it drops its copy. Each delivery is persisted
// before it is first attempted; one that fails with a network error or a
// transient HTTP status is retried with backoff by [Deliverer.Run] until it
// succeeds or the syncer's registration expires. A permanent HTTP error ends it.
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

	// Queue each delivery as already claimed by this first attempt, so a
	// retry worker doesn't pick it up while it is in flight.
	pending, err := d.store.QueueSpaceDeleted(ctx, space, regs, d.leaseUntil())
	if err != nil {
		slog.ErrorContext(ctx, "notify: queue notifySpaceDeleted", "err", err, "space", space)
		return
	}
	// Delivery outlives the originating request, so drop ctx's cancellation
	// while keeping its trace context.
	deliverCtx := context.WithoutCancel(ctx)
	for _, p := range pending {
		// The first attempt skips the still-deleted check: the deletion may
		// not have committed yet when the caller deletes inside a larger
		// transaction.
		go d.attemptSpaceDeleted(deliverCtx, p, nil)
	}
}

// DeletedSpaces reports whether a space is deleted, rather than never created
// or recreated since. spaces.Store satisfies this interface.
type DeletedSpaces interface {
	IsSpaceDeleted(ctx context.Context, uri habitat_syntax.SpaceURI) (bool, error)
}

// Run retries pending notifySpaceDeleted deliveries until ctx is done. spaces
// stops a delivery for a space recreated since it was deleted, which would
// otherwise purge the syncer's copy of the new one. Running it in several
// processes at once is safe: each delivery is claimed before it is attempted.
func (d *Deliverer) Run(ctx context.Context, spaces DeletedSpaces) error {
	ticker := time.NewTicker(retryInterval)
	defer ticker.Stop()
	for {
		d.retryDue(ctx, spaces)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// retryDue attempts every pending notifySpaceDeleted delivery that is due, a
// page at a time.
func (d *Deliverer) retryDue(ctx context.Context, spaces DeletedSpaces) {
	for ctx.Err() == nil {
		due, err := d.store.DueSpaceDeleted(ctx, d.now().UnixMilli(), retryPageSize)
		if err != nil {
			slog.ErrorContext(ctx, "notify: list due notifySpaceDeleted", "err", err)
			return
		}
		var wg sync.WaitGroup
		for _, p := range due {
			lease := d.leaseUntil()
			claimed, err := d.store.RescheduleSpaceDeleted(ctx, p, p.Attempts, lease)
			if err != nil {
				slog.ErrorContext(ctx, "notify: claim notifySpaceDeleted",
					"err", err, "space", p.Space, "audience", p.Audience)
				continue
			}
			if !claimed {
				continue
			}
			// The claim records no attempt; it only moves the row guard.
			p.RetryAt = lease
			wg.Go(func() { d.attemptSpaceDeleted(ctx, p, spaces) })
		}
		wg.Wait()
		if len(due) < retryPageSize {
			return
		}
	}
}

// attemptSpaceDeleted makes one delivery attempt of p, which the caller has
// claimed, and then clears or reschedules it. spaces is nil on the first
// attempt; see NotifySpaceDeleted.
func (d *Deliverer) attemptSpaceDeleted(
	ctx context.Context,
	p PendingDeletion,
	spaces DeletedSpaces,
) {
	log := slog.With("space", p.Space, "audience", p.Audience, "attempts", p.Attempts)
	drop := func() {
		if err := d.store.ClearSpaceDeleted(ctx, p); err != nil {
			log.ErrorContext(ctx, "notify: clear notifySpaceDeleted", "err", err)
		}
	}
	retry := func(err error) {
		// Never wait past the registration's expiry: the next attempt then
		// finds it expired and stops.
		next := min(d.nextRetryAt(p.Attempts+1), p.ExpiresAt.UnixMilli())
		if _, rerr := d.store.RescheduleSpaceDeleted(ctx, p, p.Attempts+1, next); rerr != nil {
			log.ErrorContext(ctx, "notify: reschedule notifySpaceDeleted", "err", rerr)
			return
		}
		log.WarnContext(ctx, "notify: notifySpaceDeleted queued for retry", "err", err)
	}

	if !d.now().Before(p.ExpiresAt) {
		// The syncer learns of the deletion when it renews its credential.
		log.WarnContext(ctx, "notify: notifySpaceDeleted expired with its registration")
		drop()
		return
	}
	if spaces != nil {
		deleted, err := spaces.IsSpaceDeleted(ctx, p.Space)
		if err != nil {
			retry(fmt.Errorf("check space deleted: %w", err))
			return
		}
		if !deleted {
			// Recreated since: telling the syncer now would purge its new copy.
			drop()
			return
		}
	}

	body, err := json.Marshal(habitat.NetworkHabitatSpaceNotifySpaceDeletedInput{
		Space: p.Space.String(),
	})
	if err != nil {
		log.ErrorContext(ctx, "notify: marshal notifySpaceDeleted", "err", err)
		drop()
		return
	}
	err = d.send(
		ctx,
		p.Space.SpaceOwner(),
		p.Namespace.method(methodNotifySpaceDeleted),
		p.Registration,
		body,
	)
	var permanent *permanentError
	switch {
	case err == nil:
		drop()
	case errors.As(err, &permanent):
		log.WarnContext(ctx, "notify: notifySpaceDeleted will not be retried", "err", err)
		drop()
	default:
		retry(err)
	}
}

// fanout signs a per-endpoint service-auth JWT for the space authority and
// delivers body to each registration's endpoint in the background, as the
// method named name in the registration's namespace.
func (d *Deliverer) fanout(
	ctx context.Context,
	iss syntax.DID,
	name string,
	regs []Registration,
	body []byte,
) {
	// Delivery is best-effort and outlives the originating request, so drop
	// ctx's cancellation while keeping its trace context.
	deliverCtx := context.WithoutCancel(ctx)
	for _, reg := range regs {
		go func() {
			if err := d.send(deliverCtx, iss, reg.Namespace.method(name), reg, body); err != nil {
				slog.WarnContext(deliverCtx, "notify: deliver",
					"err", err, "endpoint", reg.Endpoint, "method", name)
			}
		}()
	}
}

// permanentError marks a delivery failure that retrying won't fix.
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// retryableStatus holds the HTTP statuses a delivery is retried on, as in
// @atproto/lex's RETRYABLE_HTTP_STATUS_CODES. Any other error status is
// permanent.
var retryableStatus = map[int]bool{
	http.StatusRequestTimeout:      true,
	http.StatusTooEarly:            true,
	http.StatusTooManyRequests:     true,
	http.StatusInternalServerError: true,
	http.StatusBadGateway:          true,
	http.StatusServiceUnavailable:  true,
	http.StatusGatewayTimeout:      true,
	522:                            true, // Cloudflare: connection timed out
	524:                            true, // Cloudflare: a timeout occurred
}

// send signs a service-auth JWT from iss and delivers body to reg's endpoint as
// method. It returns a *permanentError for a failure retrying won't fix: an
// issuer this host holds no key for, or a non-retryable HTTP status. Network
// and local failures are retryable.
func (d *Deliverer) send(
	ctx context.Context,
	iss syntax.DID,
	method syntax.NSID,
	reg Registration,
	body []byte,
) error {
	// The registered endpoint is a service base URL; deliver the XRPC call to
	// <endpoint>/xrpc/<nsid> so the receiver can route and validate by method.
	url := reg.Endpoint + "/xrpc/" + method.String()
	ctx, cancel := context.WithTimeout(ctx, deliveryTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return &permanentError{fmt.Errorf("build request: %w", err)}
	}

	privKey, err := d.signer.PrivateKeyForDID(ctx, iss)
	if errors.Is(err, identity.ErrDIDNotFound) {
		// The authority isn't hosted here (any more), so nothing can sign as it.
		return &permanentError{fmt.Errorf("get private key: %w", err)}
	} else if err != nil {
		return fmt.Errorf("get private key: %w", err)
	}
	token, err := utils.ServiceAuthToken(privKey, iss, reg.audience(), &method, nil)
	if err != nil {
		return fmt.Errorf("sign service auth: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("deliver to %s: %w", reg.Endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusMultipleChoices {
		err := fmt.Errorf("delivery to %s rejected: status %d", reg.Endpoint, resp.StatusCode)
		if retryableStatus[resp.StatusCode] {
			return err
		}
		return &permanentError{err}
	}
	return nil
}

// leaseUntil is when a delivery claimed now becomes due again if its attempt
// never finishes, e.g. because the process stopped.
func (d *Deliverer) leaseUntil() int64 {
	return d.now().Add(deliveryLease).UnixMilli()
}

// nextRetryAt schedules the retry after a delivery's attempts-th failure,
// doubling from one minute up to one hour, with 50-100% jitter, as the
// reference PDS does.
func (d *Deliverer) nextRetryAt(attempts int) int64 {
	delay := min(time.Minute<<min(attempts-1, 6), time.Hour)
	jittered := delay/2 + time.Duration(rand.Int64N(int64(delay/2)+1))
	return d.now().Add(jittered).UnixMilli()
}

// audience is what a delivery's service-auth JWT is addressed to: the audience
// the subscriber registered under, i.e. its service identifier, or the endpoint
// URL for a registration predating the service field.
//
// com.atproto.space deliveries drop the service fragment and address the bare
// DID: @atproto/lex-server's serviceAuth, which com.atproto.space syncers
// verify these with, rejects any aud that isn't a plain DID.
func (r Registration) audience() string {
	if r.Namespace == NamespaceAtproto {
		did, _, _ := strings.Cut(r.Audience, "#")
		return did
	}
	return r.Audience
}
