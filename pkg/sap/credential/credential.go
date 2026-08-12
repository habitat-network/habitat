// Package credential mints, caches, and renews space credentials for sap's
// repo-host reads. A credential authorizes reads at a space's own host — the
// habitat instance its owner's repo lives on, resolved fresh per space —
// rather than an individual member's access token or home instance, so a
// syncer talks to the space's host as the space itself (authorized by the
// space's membership).
package credential

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"golang.org/x/sync/singleflight"

	"github.com/habitat-network/habitat/api/habitat"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// renewalLead is how far before a credential's nominal expiry it is renewed.
const renewalLead = 5 * time.Minute

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
	dir   Directory    // resolves a space's own host
	httpc *http.Client // for the credential exchange and repo-host reads
	deleg Delegator    // mints delegation tokens
	sf    singleflight.Group

	mu    sync.Mutex
	creds map[habitat_syntax.SpaceURI]spaceCred
}

// NewManager builds a manager. httpc must not attach any auth of its own —
// the manager sets each request's Authorization header itself, first to the
// delegation token and then to the minted space credential.
func NewManager(dir Directory, httpc *http.Client, deleg Delegator) *Manager {
	return &Manager{
		dir:   dir,
		httpc: httpc,
		deleg: deleg,
		creds: make(map[habitat_syntax.SpaceURI]spaceCred),
	}
}

// Credential returns a valid space credential for space, minting or renewing
// it as needed. Concurrent mints for the same space are deduped.
func (m *Manager) Credential(ctx context.Context, space habitat_syntax.SpaceURI) (string, error) {
	c, err := m.credential(ctx, space)
	if err != nil {
		return "", err
	}
	return c.token, nil
}

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
// a space credential there, and caches the pair. The host mints credentials
// with ~1h expiry; renewing just before expiry (see renewalLead) keeps them
// from going stale.
func (m *Manager) mint(ctx context.Context, space habitat_syntax.SpaceURI) (spaceCred, error) {
	host, err := m.hostForSpace(ctx, space)
	if err != nil {
		return spaceCred{}, fmt.Errorf("resolve space host: %w", err)
	}
	delegation, err := m.deleg.DelegationToken(ctx, space)
	if err != nil {
		return spaceCred{}, fmt.Errorf("get delegation token: %w", err)
	}
	body, err := json.Marshal(habitat.NetworkHabitatSpaceGetSpaceCredentialInput{
		Space: space.String(),
	})
	if err != nil {
		return spaceCred{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		host+"/xrpc/network.habitat.space.getSpaceCredential", bytes.NewReader(body))
	if err != nil {
		return spaceCred{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+delegation)
	resp, err := m.httpc.Do(req)
	if err != nil {
		return spaceCred{}, fmt.Errorf("get space credential: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return spaceCred{}, fmt.Errorf("get space credential: %s: %s", resp.Status, msg)
	}
	var out habitat.NetworkHabitatSpaceGetSpaceCredentialOutput
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return spaceCred{}, fmt.Errorf("decode space credential: %w", err)
	}
	c := spaceCred{token: out.Credential, host: host, expire: time.Now().Add(time.Hour)}
	m.mu.Lock()
	m.creds[space] = c
	m.mu.Unlock()
	return c, nil
}

// hostForSpace resolves the habitat host that serves space: a space's
// records live in its owner's repo, so that owner's own habitat instance is
// the only host that can mint (and later verify) a credential for it.
func (m *Manager) hostForSpace(ctx context.Context, space habitat_syntax.SpaceURI) (string, error) {
	if m.dir == nil {
		return "", errors.New("no directory configured to resolve space hosts")
	}
	owner := space.SpaceOwner()
	if owner == "" {
		return "", fmt.Errorf("space %q has no owner", space)
	}
	ident, err := m.dir.LookupDID(ctx, owner)
	if err != nil {
		return "", fmt.Errorf("lookup space owner %s: %w", owner, err)
	}
	svc, ok := ident.Services["habitat"]
	if !ok || svc.URL == "" {
		return "", fmt.Errorf("space owner %s has no habitat service endpoint", owner)
	}
	return strings.TrimSuffix(svc.URL, "/"), nil
}

// DropSpace evicts the cached credential for a deleted space.
func (m *Manager) DropSpace(space habitat_syntax.SpaceURI) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.creds, space)
}

// ClientForSpace returns an *http.Client that resolves path-only requests
// against space's own host and attaches a valid space credential to every
// request.
func (m *Manager) ClientForSpace(space habitat_syntax.SpaceURI) *http.Client {
	return &http.Client{Transport: &spaceTransport{m: m, space: space}}
}

// spaceTransport resolves path-only repo-host requests against the space's
// own host and authenticates them with the space's credential.
type spaceTransport struct {
	m     *Manager
	space habitat_syntax.SpaceURI
}

func (t *spaceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c, err := t.m.credential(req.Context(), t.space)
	if err != nil {
		return nil, err
	}
	if !req.URL.IsAbs() {
		base, err := url.Parse(c.host)
		if err != nil {
			return nil, fmt.Errorf("parse space host url: %w", err)
		}
		req.URL.Scheme = base.Scheme
		req.URL.Host = base.Host
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	return t.m.httpc.Do(req)
}
