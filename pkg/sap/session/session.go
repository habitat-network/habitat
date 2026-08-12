// Package session tracks the OAuth sessions sap syncs on behalf of, and which
// spaces each session can access. Other sap components obtain authenticated
// HTTP clients from here; they never touch session state directly.
package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/habitat-network/habitat/api/habitat"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/pkg/sap/credential"
)

// Auth methods a session can use to authenticate to its host.
const (
	AuthOAuth     = "oauth"
	AuthJWTBearer = "jwt-bearer"
)

// session is an OAuth session sap holds credentials for: any user or org
// account that completed the auth flow. sap authenticates to that account's
// host with it.
type session struct {
	DID        syntax.DID `gorm:"column:did;primaryKey"`
	SessionID  string     // keys the oauth client's session store; empty for jwt-bearer
	AuthMethod string     // AuthOAuth (default) or AuthJWTBearer
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (session) TableName() string { return "sap_sessions" }

// spaceAccess records that a session can access a space (its listSpaces
// returned it, or a notification named it). Several sessions may access the
// same space.
type spaceAccess struct {
	Space habitat_syntax.SpaceURI `gorm:"primaryKey"`
	DID   syntax.DID              `gorm:"column:did;primaryKey"`
}

func (spaceAccess) TableName() string { return "sap_space_access" }

// AutoMigrate creates the session tables.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&session{}, &spaceAccess{})
}

// JWTBearerClients builds an HTTP client that authenticates as a DID via the
// JWT-bearer grant, and resolves that DID's habitat host. Satisfied by
// jwtbearer.Builder; nil when unconfigured.
type JWTBearerClients interface {
	ClientForDID(ctx context.Context, did syntax.DID) (*http.Client, error)
	HostForDID(ctx context.Context, did syntax.DID) (string, error)
}

// Store persists sessions and space access, and builds authenticated clients.
type Store struct {
	db     *gorm.DB
	getter *getter
	jwt    JWTBearerClients // may be nil

	// jwtMgrs caches the space-credential manager for each jwt-bearer
	// session, mirroring the per-resumed-session manager an OAuth session
	// gets from getter. Keyed by DID since jwt-bearer sessions have no
	// sessionID. A pointer so WithTx copies share the same cache.
	jwtMgrs *sync.Map
}

func NewStore(db *gorm.DB, oauth *oauth.ClientApp, jwt JWTBearerClients) *Store {
	return &Store{db: db, getter: newGetter(oauth), jwt: jwt, jwtMgrs: &sync.Map{}}
}

// WithTx returns a Store scoped to the given transaction.
func (s *Store) WithTx(tx *gorm.DB) *Store {
	return &Store{db: tx, getter: s.getter, jwt: s.jwt, jwtMgrs: s.jwtMgrs}
}

// Add upserts a session for the account with the given auth method.
func (s *Store) Add(ctx context.Context, did syntax.DID, sessionID, method string) error {
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "did"}},
		DoUpdates: clause.AssignmentColumns([]string{"session_id", "auth_method", "updated_at"}),
	}).Create(&session{DID: did, SessionID: sessionID, AuthMethod: method}).Error
}

// List returns the DIDs of all sessions.
func (s *Store) List(ctx context.Context) ([]syntax.DID, error) {
	var dids []syntax.DID
	if err := s.db.WithContext(ctx).
		Model(&session{}).
		Pluck("did", &dids).Error; err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return dids, nil
}

// ClientForSession returns an HTTP client authenticated as the session's
// account against its host, using the session's recorded auth method.
func (s *Store) ClientForSession(ctx context.Context, did syntax.DID) (*http.Client, error) {
	sess, err := s.loadSession(ctx, did)
	if err != nil {
		return nil, err
	}
	if sess.AuthMethod == AuthJWTBearer {
		return s.jwtClientForDID(ctx, did)
	}
	resumed, err := s.getter.resume(ctx, sess.DID, sess.SessionID)
	if err != nil {
		return nil, err
	}
	return resumed.authClient(), nil
}

// loadSession loads the session row for a DID.
func (s *Store) loadSession(ctx context.Context, did syntax.DID) (session, error) {
	var sess session
	if err := s.db.WithContext(ctx).First(&sess, "did = ?", did).Error; err != nil {
		return session{}, fmt.Errorf("load session %s: %w", did, err)
	}
	return sess, nil
}

// jwtClientForDID returns a client authenticated as did via the JWT-bearer
// grant, or an error if sap has no builder configured.
func (s *Store) jwtClientForDID(ctx context.Context, did syntax.DID) (*http.Client, error) {
	if s.jwt == nil {
		return nil, fmt.Errorf("jwt-bearer client not configured for %s", did)
	}
	return s.jwt.ClientForDID(ctx, did)
}

// jwtManager returns the cached space-credential manager for a jwt-bearer
// session, minting it on first use. It mirrors resumed.manager for OAuth
// sessions: getDelegationToken is authorized with the session's own
// act-as-subject token — the JWT-bearer grant's whole purpose is minting an
// access token equivalent to an OAuth one — and the delegation token is then
// exchanged for a space credential, exactly as an OAuth session would.
func (s *Store) jwtManager(ctx context.Context, did syntax.DID) (*credential.Manager, error) {
	if v, ok := s.jwtMgrs.Load(did); ok {
		return v.(*credential.Manager), nil
	}
	if s.jwt == nil {
		return nil, fmt.Errorf("jwt-bearer client not configured for %s", did)
	}
	host, err := s.jwt.HostForDID(ctx, did)
	if err != nil {
		return nil, fmt.Errorf("resolve host for %s: %w", did, err)
	}
	authClient, err := s.jwt.ClientForDID(ctx, did)
	if err != nil {
		return nil, fmt.Errorf("jwt-bearer client for %s: %w", did, err)
	}
	mgr := credential.NewManager(host, &http.Client{}, jwtDelegator{client: authClient})
	actual, _ := s.jwtMgrs.LoadOrStore(did, mgr)
	return actual.(*credential.Manager), nil
}

// jwtDelegator implements credential.Delegator for a jwt-bearer session:
// client already attaches the session's own act-as-subject token to every
// request, the same auth an OAuth session's access token provides.
type jwtDelegator struct {
	client *http.Client
}

func (d jwtDelegator) DelegationToken(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"/xrpc/network.habitat.space.getDelegationToken?space="+url.QueryEscape(space.String()),
		nil)
	if err != nil {
		return "", err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("get delegation token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("get delegation token: %s", resp.Status)
	}
	var out habitat.NetworkHabitatSpaceGetDelegationTokenOutput
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode delegation token: %w", err)
	}
	return out.Token, nil
}

// RecordSpaceAccess records that the session can access the space.
func (s *Store) RecordSpaceAccess(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
) error {
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&spaceAccess{Space: space, DID: did}).Error
}

// ClientForSpace returns a client for any session that can access the space:
// the recorded accessors first, then the space owner (often — but not always
// — a session itself). Every auth method reads via a space credential (as
// the space, not as the member) — per the permissioned-data proposal,
// listRepos and the other repo-host reads this backs span every member's
// data in the space and require space-level authorization, not a single
// member's access token.
func (s *Store) ClientForSpace(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) (*http.Client, error) {
	var access []spaceAccess
	if err := s.db.WithContext(ctx).
		Where("space = ?", space).
		Find(&access).Error; err != nil {
		return nil, fmt.Errorf("load space access: %w", err)
	}

	seen := make(map[syntax.DID]struct{})
	var candidates []syntax.DID
	for _, a := range access {
		if _, ok := seen[a.DID]; !ok {
			seen[a.DID] = struct{}{}
			candidates = append(candidates, a.DID)
		}
	}
	if owner := space.SpaceOwner(); owner != "" {
		if _, ok := seen[owner]; !ok {
			candidates = append(candidates, owner)
		}
	}

	var errs []error
	for _, did := range candidates {
		sess, err := s.loadSession(ctx, did)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if sess.AuthMethod == AuthJWTBearer {
			mgr, err := s.jwtManager(ctx, did)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			return mgr.ClientForSpace(space), nil
		}
		resumed, err := s.getter.resume(ctx, sess.DID, sess.SessionID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		return resumed.credentialClient(space), nil
	}
	return nil, fmt.Errorf("no session with access to %s: %w", space, errors.Join(errs...))
}

// Spaces returns every space any session can access.
func (s *Store) Spaces(ctx context.Context) ([]habitat_syntax.SpaceURI, error) {
	var spaces []habitat_syntax.SpaceURI
	if err := s.db.WithContext(ctx).
		Model(&spaceAccess{}).
		Distinct("space").
		Pluck("space", &spaces).Error; err != nil {
		return nil, fmt.Errorf("list spaces: %w", err)
	}
	return spaces, nil
}

// DropSpace forgets all access records for a deleted space and evicts any
// cached space credentials.
func (s *Store) DropSpace(ctx context.Context, space habitat_syntax.SpaceURI) error {
	s.getter.dropSpaceCredential(space)
	s.jwtMgrs.Range(func(_, v any) bool {
		v.(*credential.Manager).DropSpace(space)
		return true
	})
	return s.db.WithContext(ctx).
		Where("space = ?", space).
		Delete(&spaceAccess{}).Error
}
