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
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"golang.org/x/sync/singleflight"

	"github.com/habitat-network/habitat/api/habitat"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
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

// ClientForSpace returns an atproto API client that reads space at its own
// host, authenticated with a valid space credential.
func (m *Manager) ClientForSpace(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) (*atclient.APIClient, error) {
	c, err := m.credential(ctx, space)
	if err != nil {
		return nil, err
	}
	return &atclient.APIClient{
		Client:  m.httpc,
		Host:    c.host,
		Headers: http.Header{"Authorization": []string{"Bearer " + c.token}},
	}, nil
}
