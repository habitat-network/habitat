package foreignrepo

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/utils"
)

const (
	notifyMethod        syntax.NSID = "com.atproto.space.notifyRepoUpdate"
	pdsService                      = "#atproto_pds"
	notifyAttemptBudget             = 10 * time.Second
	notifyTokenTTL                  = 30 * time.Second
	maxNotifyRequest                = 16 << 10
	maxNotifyResponse               = 8 << 10
	maxDIDDocument                  = 64 << 10
)

// Only tests invoke this single attempt. It does not claim or complete a
// lease, mutate the repo, or schedule retries.
type inviteNotifier struct {
	local  hive.Hive
	client *http.Client
}

func newInviteNotifier(local hive.Hive) *inviteNotifier {
	client := httpx.NewPublicClient()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &inviteNotifier{local: local, client: client}
}

// A result carries no peer-supplied text. A submission can be acknowledged,
// definitively rejected, or unconfirmed, independently of whether to retry.
type attemptReason string

const (
	reasonAccepted       attemptReason = "acknowledged"
	reasonLeaseExpired   attemptReason = "lease_expired"
	reasonInvalidScope   attemptReason = "invalid_scope"
	reasonLocalCustody   attemptReason = "local_custody"
	reasonLocalState     attemptReason = "local_state_unavailable"
	reasonLocalSigner    attemptReason = "local_signer"
	reasonResolve        attemptReason = "resolve_unavailable"
	reasonRecipient      attemptReason = "recipient_unavailable"
	reasonUnsafeEndpoint attemptReason = "unsafe_endpoint"
	reasonInterrupted    attemptReason = "response_unconfirmed"
	reasonBadResponse    attemptReason = "invalid_response"
	reasonThrottled      attemptReason = "throttled"
	reasonServer         attemptReason = "retryable_server"
	reasonUnsupported    attemptReason = "unsupported_endpoint"
	reasonRejected       attemptReason = "authority_rejected"
)

type attemptResult struct {
	outcome Outcome
	reason  attemptReason
}

func (n *inviteNotifier) attempt(ctx context.Context, item Delivery) attemptResult {
	notice := item.Notification()
	k, err := parseScope(notice.Space, notice.Author)
	if err != nil || item.attempt < 1 || item.attempt > maxAttempts {
		return attemptResult{PermanentFailure, reasonInvalidScope}
	}
	if _, err := syntax.ParseTID(notice.Rev.String()); err != nil {
		return attemptResult{PermanentFailure, reasonInvalidScope}
	}
	now := time.Now()
	leaseEnd := time.UnixMilli(min(item.leaseUntil, item.expiresAt))
	if !leaseEnd.After(now) {
		return attemptResult{Retry, reasonLeaseExpired}
	}
	ctx, cancel := context.WithDeadline(
		ctx,
		earlierDeadline(now.Add(notifyAttemptBudget), leaseEnd),
	)
	defer cancel()

	// Hive validates local custody when resolving the author's signing key.
	// Do this before resolving a foreign DID or making any network request.
	key, err := n.local.PrivateKeyForDID(ctx, k.author)
	if errors.Is(err, identity.ErrDIDNotFound) || (err == nil && key == nil) {
		return attemptResult{PermanentFailure, reasonLocalCustody}
	}
	if err != nil {
		return attemptResult{Retry, reasonLocalSigner}
	}
	owner := k.inbox.SpaceOwner()
	if _, err := n.local.LookupDID(ctx, owner); err == nil {
		return attemptResult{PermanentFailure, reasonInvalidScope}
	} else if !errors.Is(err, identity.ErrDIDNotFound) {
		return attemptResult{Retry, reasonLocalState}
	}
	endpoint, failure := n.resolveRecipient(ctx, owner)
	if endpoint == nil {
		return failure
	}
	body, err := json.Marshal(struct {
		Space   string       `json:"space"`
		Repo    syntax.DID   `json:"repo"`
		RepoRev syntax.TID   `json:"repoRev"`
		Data    atdata.Bytes `json:"data"`
	}{k.inbox.AtSpace().String(), k.author, notice.Rev, notice.Digest[:]})
	if err != nil || len(body) > maxNotifyRequest {
		return attemptResult{PermanentFailure, reasonInvalidScope}
	}
	target := endpoint.JoinPath("xrpc", notifyMethod.String())
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		target.String(),
		bytes.NewReader(body),
	)
	if err != nil {
		return attemptResult{PermanentFailure, reasonUnsafeEndpoint}
	}
	token, err := utils.ServiceAuthToken(
		key,
		k.author,
		owner.String()+pdsService,
		new(notifyMethod),
		new(notifyTokenTTL),
	)
	if err != nil {
		return attemptResult{PermanentFailure, reasonLocalSigner}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if unsafeNotifyTransport(err) {
		return attemptResult{PermanentFailure, reasonUnsafeEndpoint}
	}
	if err != nil {
		// The server may already have committed before the response was lost.
		return attemptResult{Retry, reasonInterrupted}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return attemptResult{PermanentFailure, reasonUnsafeEndpoint}
	}
	response, err := readBounded(resp.Body, maxNotifyResponse)
	if errors.Is(err, errResponseTooLarge) {
		return attemptResult{PermanentFailure, reasonBadResponse}
	}
	if err != nil {
		return attemptResult{Retry, reasonInterrupted}
	}
	return notificationOutcome(resp.StatusCode, response)
}

func notificationOutcome(status int, body []byte) attemptResult {
	switch {
	case status == http.StatusOK && len(body) == 0:
		// The pinned XRPC method declares no output and sends an empty 200.
		return attemptResult{Delivered, reasonAccepted}
	case status >= 200 && status < 300:
		return attemptResult{PermanentFailure, reasonBadResponse}
	case status == http.StatusTooManyRequests:
		return attemptResult{Retry, reasonThrottled}
	case status == http.StatusRequestTimeout:
		return attemptResult{Retry, reasonInterrupted}
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return attemptResult{PermanentFailure, reasonRejected}
	}
	// Keep only a fixed XRPC error name. In particular, bare 404 is not
	// evidence that an inbox was found and rejected.
	var xrpc struct {
		Error string `json:"error"`
	}
	if status >= 400 && status < 500 && json.Unmarshal(body, &xrpc) == nil &&
		xrpc.Error == "SpaceNotFound" {
		return attemptResult{PermanentFailure, reasonRejected}
	}
	switch {
	case status == http.StatusNotFound || status == http.StatusMethodNotAllowed || status == http.StatusNotImplemented:
		return attemptResult{PermanentFailure, reasonUnsupported}
	case status >= 500 && status < 600:
		return attemptResult{Retry, reasonServer}
	case status >= 400 && status < 500:
		return attemptResult{PermanentFailure, reasonRejected}
	default:
		return attemptResult{PermanentFailure, reasonBadResponse}
	}
}

func (n *inviteNotifier) resolveRecipient(
	ctx context.Context,
	owner syntax.DID,
) (*url.URL, attemptResult) {
	invalid := attemptResult{PermanentFailure, reasonRecipient}
	retry := attemptResult{Retry, reasonResolve}
	unsafe := attemptResult{PermanentFailure, reasonUnsafeEndpoint}
	var documentURL url.URL
	switch owner.Method() {
	case "plc":
		documentURL = url.URL{Scheme: "https", Host: "plc.directory", Path: "/" + owner.String()}
	case "web":
		// Like Indigo's atproto DID resolver, did:web accounts must use
		// a simple hostname with an allowed TLD, not a port or DID path.
		host, err := syntax.ParseHandle(owner.Identifier())
		if err != nil || !host.AllowedTLD() {
			return nil, invalid
		}
		documentURL = url.URL{Scheme: "https", Host: host.String(), Path: "/.well-known/did.json"}
	default:
		return nil, invalid
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, documentURL.String(), http.NoBody)
	if err != nil {
		return nil, invalid
	}
	resp, err := n.client.Do(req)
	if unsafeNotifyTransport(err) {
		return nil, unsafe
	}
	if err != nil {
		return nil, retry
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return nil, unsafe
	case resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests ||
		resp.StatusCode >= 500:
		return nil, retry
	case resp.StatusCode != http.StatusOK:
		return nil, invalid
	}
	b, err := readBounded(resp.Body, maxDIDDocument)
	if errors.Is(err, errResponseTooLarge) {
		return nil, invalid
	}
	if err != nil {
		return nil, retry
	}
	var doc identity.DIDDocument
	if json.Unmarshal(b, &doc) != nil || doc.DID != owner {
		return nil, invalid
	}
	var endpoint string
	found := false
	for _, s := range doc.Service {
		if s.ID != pdsService && s.ID != owner.String()+pdsService {
			continue
		}
		if found || s.Type != "AtprotoPersonalDataServer" {
			return nil, invalid
		}
		endpoint, found = s.ServiceEndpoint, true
	}
	if !found || endpoint == "" {
		return nil, invalid
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Opaque != "" || u.Hostname() == "" ||
		u.User != nil || u.ForceQuery || u.RawQuery != "" || strings.Contains(endpoint, "#") {
		return nil, unsafe
	}
	if addr, err := netip.ParseAddr(u.Hostname()); err == nil && !httpx.IsPublicAddr(addr) {
		return nil, unsafe
	}
	return u, attemptResult{}
}

var errResponseTooLarge = errors.New("notification response exceeds size limit")

func unsafeNotifyTransport(err error) bool {
	var badCertificate *tls.CertificateVerificationError
	return errors.Is(err, httpx.ErrNonPublicAddress) || errors.As(err, &badCertificate)
}

func readBounded(body io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(body, limit+1))
	if int64(len(b)) > limit {
		return nil, errResponseTooLarge
	}
	return b, err
}

func earlierDeadline(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
