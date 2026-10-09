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
	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/sync/singleflight"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/httpsig"
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

// credKey identifies a cached credential: bound credentials are tied to the
// manager's signing key and usable only through ClientForSpace; unbound ones
// are bearer credentials handed to callers that read the host themselves.
type credKey struct {
	space habitat_syntax.SpaceURI
	bound bool
}

// signingTransport signs every outgoing request per RFC 9421 with key,
// covering components. For credential use it also sets the
// Atproto-Space-Audience header, which the signature covers.
type signingTransport struct {
	base       http.RoundTripper
	key        atcrypto.PrivateKey
	audience   syntax.DID // empty when the request doesn't carry an audience
	components []string
}

func (t signingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// RoundTrippers must not modify the caller's request.
	req = req.Clone(req.Context())
	if t.audience != "" {
		req.Header.Set(httpsig.AudienceHeader, t.audience.String())
	}
	if err := httpsig.Sign(req, t.key, t.components...); err != nil {
		return nil, fmt.Errorf("sign request: %w", err)
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

// signedClient returns a copy of httpc whose requests are signed by key.
func signedClient(
	httpc *http.Client,
	key atcrypto.PrivateKey,
	audience syntax.DID,
	components ...string,
) *http.Client {
	c := *httpc
	c.Transport = signingTransport{
		base: httpc.Transport, key: key, audience: audience, components: components,
	}
	return &c
}

// Manager mints and caches space credentials, one per space, each scoped to
// that space's own host. Credentials are minted lazily on first use, shared
// across callers via singleflight, and renewed just before they expire.
// Credentials used through ClientForSpace are bound to a P-256 key the manager
// generates at startup (cnf.kid) and each request is signed per RFC 9421.
type Manager struct {
	dir      Directory           // resolves a space's own host
	httpc    *http.Client        // for the credential exchange and repo-host reads
	deleg    Delegator           // mints delegation tokens
	attester *oauth.ClientConfig // signs client attestations; must be a confidential client
	keyOnce  sync.Once
	key      atcrypto.PrivateKey // P-256 key bound credentials are tied to (cnf.kid)
	keyErr   error
	sf       singleflight.Group

	mu    sync.Mutex
	creds map[credKey]spaceCred
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
		creds:    make(map[credKey]spaceCred),
	}
}

// signingKey returns the manager's credential binding key, generating it on
// first use.
func (m *Manager) signingKey() (atcrypto.PrivateKey, error) {
	m.keyOnce.Do(func() {
		m.key, m.keyErr = atcrypto.GeneratePrivateKeyP256()
	})
	if m.keyErr != nil {
		return nil, fmt.Errorf("generate credential binding key: %w", m.keyErr)
	}
	return m.key, nil
}

// credential returns a valid space credential for space, minting or renewing
// it as needed. Concurrent mints for the same space are deduped.
func (m *Manager) credential(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	bound bool,
) (spaceCred, error) {
	k := credKey{space: space, bound: bound}
	m.mu.Lock()
	c, ok := m.creds[k]
	m.mu.Unlock()
	if ok && time.Until(c.expire) > renewalLead {
		return c, nil
	}
	v, err, _ := m.sf.Do(fmt.Sprint(k), func() (any, error) {
		return m.mint(ctx, space, bound)
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
func (m *Manager) mint(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	bound bool,
) (spaceCred, error) {
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
	httpc := m.httpc
	if bound {
		// Signing the delegation token proves we hold the key the credential
		// will be bound to.
		key, err := m.signingKey()
		if err != nil {
			return spaceCred{}, err
		}
		httpc = signedClient(m.httpc, key, "", "authorization")
	}
	client := &atclient.APIClient{
		Client:  httpc,
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
	m.creds[credKey{space: space, bound: bound}] = c
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
	stale := strings.TrimPrefix(req.Header.Get("Authorization"), httpsig.CredentialScheme+" ")
	t.m.invalidate(t.space, stale)
	c, err := t.m.credential(req.Context(), t.space, true)
	if err != nil || c.token == stale {
		return resp, nil
	}
	retry := req.Clone(req.Context())
	if req.GetBody != nil {
		if retry.Body, err = req.GetBody(); err != nil {
			return resp, nil
		}
	}
	// next re-signs the retry: the signature covers the new credential.
	retry.Header.Set("Authorization", httpsig.CredentialScheme+" "+c.token)
	_ = resp.Body.Close()
	return t.next.RoundTrip(retry)
}

// invalidate evicts space's cached bound credential if it is still token.
func (m *Manager) invalidate(space habitat_syntax.SpaceURI, token string) {
	k := credKey{space: space, bound: true}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.creds[k].token == token {
		delete(m.creds, k)
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
	delete(m.creds, credKey{space: space, bound: true})
	delete(m.creds, credKey{space: space, bound: false})
}

// Credential is an unbound (bearer) space credential and the space host it is
// valid against.
type Credential struct {
	Token string
	Host  string
}

// Credential returns a valid unbound space credential for space, for a caller
// that reads the space's host itself rather than through ClientForSpace: it
// has no access to the manager's signing key, so it can't use a bound one.
func (m *Manager) Credential(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) (Credential, error) {
	c, err := m.credential(ctx, space, false)
	if err != nil {
		return Credential{}, err
	}
	return Credential{Token: c.token, Host: c.host}, nil
}

// ClientForSpace returns an atproto API client that reads space at its own
// host, authenticated with a valid key-bound space credential. Every request
// is signed per RFC 9421 over the credential and its audience, the space
// owner's DID. A request rejected with 401 is retried once with a freshly
// minted credential.
func (m *Manager) ClientForSpace(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) (*atclient.APIClient, error) {
	c, err := m.credential(ctx, space, true)
	if err != nil {
		return nil, err
	}
	key, err := m.signingKey()
	if err != nil {
		return nil, err
	}
	// The refresh transport sits above the signing one so a retried request,
	// carrying a new credential, is signed again.
	signed := signedClient(
		m.httpc, key, space.SpaceOwner(), "authorization", "atproto-space-audience",
	)
	httpc := *m.httpc
	httpc.Transport = &refreshTransport{m: m, space: space, next: signed.Transport}
	return &atclient.APIClient{
		Client: &httpc,
		Host:   c.host,
		Headers: http.Header{
			"Authorization": []string{httpsig.CredentialScheme + " " + c.token},
		},
	}, nil
}
