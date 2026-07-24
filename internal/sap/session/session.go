// Package session tracks the OAuth sessions sap syncs on behalf of, and which
// spaces each session can access. Other sap components obtain authenticated
// HTTP clients from here; they never touch session state directly.
package session

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
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
// JWT-bearer grant. Satisfied by jwtbearer.Builder; nil when unconfigured.
type JWTBearerClients interface {
	ClientForDID(ctx context.Context, did syntax.DID) (*http.Client, error)
}

// Store persists sessions and space access, and builds authenticated clients.
type Store struct {
	db     *gorm.DB
	getter *getter
	jwt    JWTBearerClients // may be nil
}

func NewStore(db *gorm.DB, oauth *oauth.ClientApp, jwt JWTBearerClients) *Store {
	return &Store{db: db, getter: newGetter(oauth), jwt: jwt}
}

// WithTx returns a Store scoped to the given transaction.
func (s *Store) WithTx(tx *gorm.DB) *Store {
	return &Store{db: tx, getter: s.getter, jwt: s.jwt}
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
	var sess session
	if err := s.db.WithContext(ctx).First(&sess, "did = ?", did).Error; err != nil {
		return nil, fmt.Errorf("load session %s: %w", did, err)
	}
	if sess.AuthMethod == AuthJWTBearer {
		if s.jwt == nil {
			return nil, fmt.Errorf("jwt-bearer client not configured for %s", did)
		}
		return s.jwt.ClientForDID(ctx, did)
	}
	resumed, err := s.getter.resume(ctx, sess.DID, sess.SessionID)
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

// ClientForSpace returns a client for any session that can access the space:
// the recorded accessors first, then the space owner (often — but not always —
// a session itself).
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
		client, err := s.ClientForSession(ctx, did)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		return client, nil
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

// DropSpace forgets all access records for a deleted space.
func (s *Store) DropSpace(ctx context.Context, space habitat_syntax.SpaceURI) error {
	return s.db.WithContext(ctx).
		Where("space = ?", space).
		Delete(&spaceAccess{}).Error
}
