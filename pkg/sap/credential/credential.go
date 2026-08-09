// Package credential mints, caches, and renews per-space host credentials for
// sap's repo-host reads, so a syncer talks to the host as the space (authorized
// by the space's membership) rather than as an individual member's OAuth
// session.
package credential

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/habitat-network/habitat/api/habitat"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// renewalLead is how far before a credential's nominal expiry it is renewed.
const renewalLead = 5 * time.Minute

// Delegator mints a short-lived delegation token for a space. The session's
// OAuth access authorizes it (getDelegationToken is OAuth-only).
type Delegator interface {
	DelegationToken(ctx context.Context, space habitat_syntax.SpaceURI) (string, error)
}

// Manager mints and caches space credentials for one host (the session's
// Habitat instance). Credentials are minted lazily on first use, shared across
// callers via singleflight, and renewed just before they expire.
type Manager struct {
	host  string       // host base URL (scheme + host)
	httpc *http.Client // for the credential exchange and repo-host reads
	deleg Delegator    // mints delegation tokens
	sf    singleflight.Group

	mu     sync.Mutex
	creds  map[habitat_syntax.SpaceURI]string
	expire map[habitat_syntax.SpaceURI]time.Time
}

// NewManager builds a manager for one host.
func NewManager(host string, httpc *http.Client, deleg Delegator) *Manager {
	return &Manager{
		host:   host,
		httpc:  httpc,
		deleg:  deleg,
		creds:  make(map[habitat_syntax.SpaceURI]string),
		expire: make(map[habitat_syntax.SpaceURI]time.Time),
	}
}

// Credential returns a valid space credential for space, minting or renewing
// it as needed. Concurrent mints for the same space are deduped.
func (m *Manager) Credential(ctx context.Context, space habitat_syntax.SpaceURI) (string, error) {
	m.mu.Lock()
	token, exp := m.creds[space], m.expire[space]
	m.mu.Unlock()
	if token != "" && time.Until(exp) > renewalLead {
		return token, nil
	}
	tok, err, _ := m.sf.Do(space.String(), func() (any, error) {
		return m.mint(ctx, space)
	})
	if err != nil {
		return "", err
	}
	return tok.(string), nil
}

// mint exchanges a fresh delegation token for a space credential. The host
// mints credentials with ~1h expiry; renewing just before expiry (see
// renewalLead) keeps them from going stale.
func (m *Manager) mint(ctx context.Context, space habitat_syntax.SpaceURI) (string, error) {
	delegation, err := m.deleg.DelegationToken(ctx, space)
	if err != nil {
		return "", fmt.Errorf("get delegation token: %w", err)
	}
	body, err := json.Marshal(habitat.NetworkHabitatSpaceGetSpaceCredentialInput{
		Space: space.String(),
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		m.abs("/xrpc/network.habitat.space.getSpaceCredential"), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+delegation)
	resp, err := m.httpc.Do(req)
	if err != nil {
		return "", fmt.Errorf("get space credential: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("get space credential: %s: %s", resp.Status, msg)
	}
	var out habitat.NetworkHabitatSpaceGetSpaceCredentialOutput
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode space credential: %w", err)
	}
	m.mu.Lock()
	m.creds[space] = out.Credential
	m.expire[space] = time.Now().Add(time.Hour)
	m.mu.Unlock()
	return out.Credential, nil
}

// DropSpace evicts the cached credential for a deleted space.
func (m *Manager) DropSpace(space habitat_syntax.SpaceURI) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.creds, space)
	delete(m.expire, space)
}

// ClientForSpace returns an *http.Client that attaches a valid space
// credential to every request for space.
func (m *Manager) ClientForSpace(space habitat_syntax.SpaceURI) *http.Client {
	return &http.Client{Transport: &spaceTransport{m: m, space: space}}
}

// spaceTransport resolves path-only repo-host requests against the host and
// authenticates them with the space's credential.
type spaceTransport struct {
	m     *Manager
	space habitat_syntax.SpaceURI
}

func (t *spaceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !req.URL.IsAbs() {
		base, err := url.Parse(t.m.host)
		if err != nil {
			return nil, fmt.Errorf("parse host url: %w", err)
		}
		req.URL.Scheme = base.Scheme
		req.URL.Host = base.Host
	}
	token, err := t.m.Credential(req.Context(), t.space)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return t.m.httpc.Do(req)
}

func (m *Manager) abs(path string) string {
	return m.host + path
}
