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

// JWTBearer mints a session for a DID via the RFC 7523 JWT-bearer grant,
// persisting it the same way a completed browser OAuth flow would — the
// returned session is then resumable like any other, through the same
// oauth.ClientApp store. Satisfied by *oauthclient.Client; nil when
// unconfigured.
type JWTBearer interface {
	SendJWTTokenRequest(ctx context.Context, identifier string) (*oauth.ClientSessionData, error)
}

// Store persists sessions and space access, and builds authenticated clients.
type Store struct {
	db     *gorm.DB
	getter *getter
	jwt    JWTBearer // may be nil

	// mgr mints and caches space credentials, one per space regardless of
	// which session was used to obtain it — a space credential authorizes
	// the space, not the member who fetched it. Store implements
	// credential.Delegator so mgr can ask any accessing session for a
	// delegation token on demand.
	mgr *credential.Manager
}

func NewStore(
	db *gorm.DB,
	oauth *oauth.ClientApp,
	jwt JWTBearer,
	dir credential.Directory,
) *Store {
	s := &Store{db: db, getter: newGetter(oauth), jwt: jwt}
	s.mgr = credential.NewManager(dir, &http.Client{}, s)
	return s
}

// WithTx returns a Store scoped to the given transaction. The credential
// manager and OAuth session cache are shared, not tx-scoped: they're
// in-memory HTTP client state, not data this transaction writes.
func (s *Store) WithTx(tx *gorm.DB) *Store {
	return &Store{db: tx, getter: s.getter, jwt: s.jwt, mgr: s.mgr}
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
		return s.jwtBearerClient(ctx, sess)
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

// jwtBearerClient returns a client authenticated as sess's DID via the
// JWT-bearer grant. A session minted by an earlier call is resumed like any
// OAuth session; only the first call (or one after the minted session stops
// resuming, e.g. it expired) actually performs the grant exchange.
func (s *Store) jwtBearerClient(ctx context.Context, sess session) (*http.Client, error) {
	if sess.SessionID != "" {
		if resumed, err := s.getter.resume(ctx, sess.DID, sess.SessionID); err == nil {
			return resumed.authClient(), nil
		}
	}
	if s.jwt == nil {
		return nil, fmt.Errorf("jwt-bearer client not configured for %s", sess.DID)
	}
	sessData, err := s.jwt.SendJWTTokenRequest(ctx, sess.DID.String())
	if err != nil {
		return nil, fmt.Errorf("mint jwt-bearer session for %s: %w", sess.DID, err)
	}
	if err := s.db.WithContext(ctx).Model(&session{}).
		Where("did = ?", sess.DID).
		Update("session_id", sessData.SessionID).Error; err != nil {
		return nil, fmt.Errorf("save jwt-bearer session id: %w", err)
	}
	resumed, err := s.getter.resume(ctx, sess.DID, sessData.SessionID)
	if err != nil {
		return nil, err
	}
	return resumed.authClient(), nil
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

// ClientForSpace returns a client that reads space at its own host —
// resolved from the space owner's DID, since a space's records live in its
// owner's repo — authenticated with a space credential. Per the
// permissioned-data proposal, listRepos and the other repo-host reads this
// backs (listRepoOps, getRepo, registerNotify) span every member's data in
// the space and require that space-level authorization, not a single
// member's access token.
//
// The credential is minted lazily on first use and cached thereafter (see
// DelegationToken), so this never itself fails for lack of a session; it
// only fails once a request actually needs a credential no accessible
// session could obtain.
func (s *Store) ClientForSpace(
	_ context.Context,
	space habitat_syntax.SpaceURI,
) (*http.Client, error) {
	return s.mgr.ClientForSpace(space), nil
}

// DelegationToken implements credential.Delegator: it picks a session that
// can access space — a recorded accessor, falling back to the space owner —
// and asks that session's own host for a delegation token, authenticated
// with the session's own access token (see ClientForSession). Candidates are
// tried in order until one succeeds.
func (s *Store) DelegationToken(ctx context.Context, space habitat_syntax.SpaceURI) (string, error) {
	candidates, err := s.candidatesForSpace(ctx, space)
	if err != nil {
		return "", err
	}
	var errs []error
	for _, did := range candidates {
		client, err := s.ClientForSession(ctx, did)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		token, err := fetchDelegationToken(ctx, client, space)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		return token, nil
	}
	return "", fmt.Errorf(
		"no session could get a delegation token for %s: %w", space, errors.Join(errs...))
}

// candidatesForSpace returns the DIDs to try for a space read: sessions with
// recorded access first, then the space owner as a fallback (it may not have
// listed the space to itself via listSpaces, e.g. if sap only learned of the
// space from a notifyWrite naming some other repo).
func (s *Store) candidatesForSpace(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) ([]syntax.DID, error) {
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
	return candidates, nil
}

// fetchDelegationToken calls getDelegationToken for space using client,
// which must already authenticate its own requests (see ClientForSession).
func fetchDelegationToken(
	ctx context.Context,
	client *http.Client,
	space habitat_syntax.SpaceURI,
) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"/xrpc/network.habitat.space.getDelegationToken?space="+url.QueryEscape(space.String()),
		nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
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

// DropSpace forgets all access records for a deleted space and evicts its
// cached space credential.
func (s *Store) DropSpace(ctx context.Context, space habitat_syntax.SpaceURI) error {
	s.mgr.DropSpace(space)
	return s.db.WithContext(ctx).
		Where("space = ?", space).
		Delete(&spaceAccess{}).Error
}
