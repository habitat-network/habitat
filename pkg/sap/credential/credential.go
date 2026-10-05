// Package credential mints, caches, and renews space credentials for sap's
// repo-host reads. A credential authorizes reads at a space's own host — the
// habitat instance its owner's repo lives on, resolved fresh per space —
// rather than an individual member's access token or home instance, so a
// syncer talks to the space's host as the space itself (authorized by the
// space's membership).
package credential

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/sync/singleflight"

	"github.com/habitat-network/habitat/api/habitat"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
)

// renewalLead is how far before a credential's expiry it is renewed. Hosts
// mint credentials with a 10 minute default lifetime, so this keeps a cached
// credential from being used in its last couple of minutes.
const renewalLead = 2 * time.Minute

// defaultCredentialTTL is assumed for a credential whose exp claim can't be
// read: the spec's default lifetime.
const defaultCredentialTTL = 10 * time.Minute

// Delegator mints a short-lived delegation token authorizing a read of
// space, using some session that can access it. getDelegationToken is served
// by that session's own host, authenticated with the session's own access
// token (OAuth, or an equivalent JWT-bearer token) — never with the space
// credential this token is later exchanged for.
type Delegator interface {
	DelegationToken(ctx context.Context, space habitat_syntax.SpaceURI) (string, error)
}

// Directory resolves a space's own host: the habitat instance its owner's
// repo lives on, which is the only host that can mint or verify a credential
// for that space. Satisfied by identity.Directory.
type Directory interface {
	LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error)
}

// spaceCred is a cached credential for one space, paired with the host it
// was minted for and is only valid against.
type spaceCred struct {
	token  string
	host   string
	expire time.Time
}

// Manager mints and caches space credentials, one per space, each scoped to
// that space's own host. Credentials are minted lazily on first use, shared
// across callers via singleflight, and renewed just before they expire.
type Manager struct {
	dir      Directory           // resolves a space's own host
	httpc    *http.Client        // for the credential exchange and repo-host reads
	deleg    Delegator           // mints delegation tokens
	attester *oauth.ClientConfig // signs client attestations; must be a confidential client
	sf       singleflight.Group

	mu    sync.Mutex
	creds map[habitat_syntax.SpaceURI]spaceCred
}

// NewManager builds a manager. httpc must not attach any auth of its own —
// the manager sets each request's Authorization header itself, first to the
// delegation token and then to the minted space credential. attester signs
// getSpaceCredential's client attestation with the same confidential-client
// key it already publishes at its own client-metadata.json; it must be
// confidential (a configured client secret) — sap always runs as one.
func NewManager(
	dir Directory,
	httpc *http.Client,
	deleg Delegator,
	attester *oauth.ClientConfig,
) *Manager {
	return &Manager{
		dir:      dir,
		httpc:    httpc,
		deleg:    deleg,
		attester: attester,
		creds:    make(map[habitat_syntax.SpaceURI]spaceCred),
	}
}

// credential returns a valid space credential for space, minting or renewing
// it as needed. Concurrent mints for the same space are deduped.
func (m *Manager) credential(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) (spaceCred, error) {
	m.mu.Lock()
	c, ok := m.creds[space]
	m.mu.Unlock()
	if ok && time.Until(c.expire) > renewalLead {
		return c, nil
	}
	v, err, _ := m.sf.Do(space.String(), func() (any, error) {
		return m.mint(ctx, space)
	})
	if err != nil {
		return spaceCred{}, err
	}
	return v.(spaceCred), nil
}

// mint resolves the space's own host, exchanges a fresh delegation token for
// a space credential there, and caches the pair. The host mints short-lived
// credentials (10 minutes by default); renewing just before the credential's
// exp (see renewalLead) keeps them from going stale.
func (m *Manager) mint(ctx context.Context, space habitat_syntax.SpaceURI) (spaceCred, error) {
	host, err := m.hostForSpace(ctx, space)
	if err != nil {
		return spaceCred{}, fmt.Errorf("resolve space host: %w", err)
	}
	delegation, err := m.deleg.DelegationToken(ctx, space)
	if err != nil {
		return spaceCred{}, fmt.Errorf("get delegation token: %w", err)
	}
	attestation, err := m.attester.NewClientAssertion(
		space.SpaceOwner().String() + "#atproto_space_host",
	)
	if err != nil {
		return spaceCred{}, fmt.Errorf("sign client attestation: %w", err)
	}
	client := &atclient.APIClient{
		Client:  m.httpc,
		Host:    host,
		Headers: http.Header{"Authorization": []string{"Bearer " + delegation}},
	}
	var out habitat.NetworkHabitatSpaceGetSpaceCredentialOutput
	if err := client.Post(
		ctx, "network.habitat.space.getSpaceCredential",
		habitat.NetworkHabitatSpaceGetSpaceCredentialInput{
			Space: space.String(), ClientAttestation: attestation,
		}, &out,
	); err != nil {
		return spaceCred{}, fmt.Errorf("get space credential: %w", err)
	}
	c := spaceCred{token: out.Credential, host: host, expire: credentialExpiry(out.Credential)}
	m.mu.Lock()
	m.creds[space] = c
	m.mu.Unlock()
	return c, nil
}

// credentialExpiry reads a credential's exp claim without verifying it (the
// space host verifies on use), falling back to the default lifetime for a
// token that doesn't carry one.
func credentialExpiry(token string) time.Time {
	claims := jwt.RegisteredClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(token, &claims); err == nil &&
		claims.ExpiresAt != nil {
		return claims.ExpiresAt.Time
	}
	return time.Now().Add(defaultCredentialTTL)
}

// refreshTransport retries a space host request once with a freshly minted
// credential when the host rejects the one it carried with a 401: an expired
// or revoked credential (see com.atproto.space.notifyCredentialRevoked) is
// handled by re-fetching rather than surfacing the failure.
type refreshTransport struct {
	m     *Manager
	space habitat_syntax.SpaceURI
	next  http.RoundTripper
}

func (t *refreshTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(req)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	// A request body that can't be replayed can't be retried.
	if req.Body != nil && req.GetBody == nil {
		return resp, nil
	}
	stale := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
	t.m.invalidate(t.space, stale)
	c, err := t.m.credential(req.Context(), t.space)
	if err != nil || c.token == stale {
		return resp, nil
	}
	retry := req.Clone(req.Context())
	if req.GetBody != nil {
		if retry.Body, err = req.GetBody(); err != nil {
			return resp, nil
		}
	}
	retry.Header.Set("Authorization", "Bearer "+c.token)
	_ = resp.Body.Close()
	return t.next.RoundTrip(retry)
}

// invalidate evicts space's cached credential if it is still token.
func (m *Manager) invalidate(space habitat_syntax.SpaceURI, token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.creds[space].token == token {
		delete(m.creds, space)
	}
}

// hostForSpace resolves the habitat host that serves space: a space's
// records live in its owner's repo, so that owner's own habitat instance is
// the only host that can mint (and later verify) a credential for it.
func (m *Manager) hostForSpace(ctx context.Context, space habitat_syntax.SpaceURI) (string, error) {
	owner := space.SpaceOwner()
	ident, err := m.dir.LookupDID(ctx, owner)
	if err != nil {
		return "", fmt.Errorf("lookup space owner %s: %w", owner, err)
	}
	host := utils.SpaceHostEndpoint(ident)
	if host == "" {
		return "", fmt.Errorf("space owner %s has no space host service", owner)
	}
	return host, nil
}

// DropSpace evicts the cached credential for a deleted space.
func (m *Manager) DropSpace(space habitat_syntax.SpaceURI) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.creds, space)
}

// Credential is a space credential and the space host it is valid against.
type Credential struct {
	Token string
	Host  string
}

// Credential returns a valid space credential for space, for a caller that
// reads the space's host itself rather than through ClientForSpace.
func (m *Manager) Credential(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) (Credential, error) {
	c, err := m.credential(ctx, space)
	if err != nil {
		return Credential{}, err
	}
	return Credential{Token: c.token, Host: c.host}, nil
}

// ClientForSpace returns an atproto API client that reads space at its own
// host, authenticated with a valid space credential.
func (m *Manager) ClientForSpace(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) (*atclient.APIClient, error) {
	c, err := m.Credential(ctx, space)
	if err != nil {
		return nil, err
	}
	next := m.httpc.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	httpc := *m.httpc
	httpc.Transport = &refreshTransport{m: m, space: space, next: next}
	return &atclient.APIClient{
		Client:  &httpc,
		Host:    c.Host,
		Headers: http.Header{"Authorization": []string{"Bearer " + c.Token}},
	}, nil
}
