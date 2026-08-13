// Package session tracks which DIDs sap syncs on behalf of, which OAuth
// session resumes each, and which spaces each can access. It does not itself
// know how a session was established (browser OAuth flow, JWT-bearer grant,
// ...) — that happens out of band, entirely the caller's concern, which
// calls AddSession with the session ID once it has one; this package's job
// is resuming it via the caller's *oauth.ClientApp and recording space
// access on top.
//
// Store implements credential.Delegator (see DelegationToken) so a
// credential.Manager built elsewhere can ask it for delegation tokens; Store
// itself has no notion of space credentials.
//
// DelegationToken always resumes an explicit (DID, session ID) pair — never
// just a DID — so it names the exact session it means instead of guessing
// which session a DID resolves to. DelegationToken picks from among recorded
// accessors, but every candidate it tries comes from spaceAccess as an
// already-paired (DID, session ID), never a bare DID resolved separately.
package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/habitat-network/habitat/api/habitat"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Session names a session sap tracks: did, resumable via sessionID.
type Session struct {
	DID       syntax.DID
	SessionID string
}

// session is a DID sap tracks for backfill/sync, resumable via SessionID
// through the Store's *oauth.ClientApp.
type session struct {
	DID       syntax.DID `gorm:"column:did;primaryKey"`
	SessionID string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (session) TableName() string { return "sap_sessions" }

// spaceAccess records that a session can access a space (its listSpaces
// returned it, or a notification named it), and which session that was, so a
// later delegation-token exchange for the space can name a session that is
// actually known to work rather than guessing. Several sessions may access
// the same space; the most recently recorded one wins.
type spaceAccess struct {
	Space     habitat_syntax.SpaceURI `gorm:"primaryKey"`
	DID       syntax.DID              `gorm:"column:did;primaryKey"`
	SessionID string
}

func (spaceAccess) TableName() string { return "sap_space_access" }

// AutoMigrate creates the session tables.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&session{}, &spaceAccess{})
}

// Store tracks sessions and space access.
type Store struct {
	db          *gorm.DB
	oauthClient *oauth.ClientApp
}

func NewStore(db *gorm.DB, oauthClient *oauth.ClientApp) *Store {
	return &Store{db: db, oauthClient: oauthClient}
}

// WithTx returns a Store scoped to the given transaction.
func (s *Store) WithTx(tx *gorm.DB) *Store {
	return &Store{db: tx, oauthClient: s.oauthClient}
}

// Add starts tracking did for backfill/sync, resumable via sessionID. Safe
// to call again for a DID already tracked — e.g. re-authenticating after
// its session expired — replacing the session it resumes.
func (s *Store) Add(ctx context.Context, did syntax.DID, sessionID string) error {
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "did"}},
		DoUpdates: clause.AssignmentColumns([]string{"session_id"}),
	}).Create(&session{DID: did, SessionID: sessionID}).Error
}

// List returns every tracked session as a (DID, session ID) pair.
func (s *Store) List(ctx context.Context) ([]Session, error) {
	var tracked []session
	if err := s.db.WithContext(ctx).Find(&tracked).Error; err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	sessions := make([]Session, len(tracked))
	for i, t := range tracked {
		sessions[i] = Session{DID: t.DID, SessionID: t.SessionID}
	}
	return sessions, nil
}

// RecordSpaceAccess records that (did, sessionID) can access the space.
func (s *Store) RecordSpaceAccess(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
	sessionID string,
) error {
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "space"}, {Name: "did"}},
			DoUpdates: clause.AssignmentColumns([]string{"session_id"}),
		}).
		Create(&spaceAccess{Space: space, DID: did, SessionID: sessionID}).Error
}

// DelegationToken implements credential.Delegator: it tries each session on
// record as having access to space — via spaceAccess, which already pairs a
// DID with the session ID that recorded its access — asking that session's
// own host for a delegation token, authenticated with the session's own
// access token (resumed through the Store's *oauth.ClientApp). Candidates are
// tried in order until one succeeds.
func (s *Store) DelegationToken(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) (string, error) {
	candidates, err := s.candidatesForSpace(ctx, space)
	if err != nil {
		return "", err
	}
	var errs []error
	for _, candidate := range candidates {
		sess, err := s.oauthClient.ResumeSession(ctx, candidate.DID, candidate.SessionID)
		if err != nil {
			errs = append(errs, fmt.Errorf(
				"resume session %s for %s: %w", candidate.SessionID, candidate.DID, err,
			))
			continue
		}
		token, err := fetchDelegationToken(ctx, sess.APIClient(), space)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		return token, nil
	}
	return "", fmt.Errorf(
		"no session could get a delegation token for %s: %w", space, errors.Join(errs...),
	)
}

// candidatesForSpace returns the (DID, session ID) pairs on record as having
// access to space — each one drawn directly from spaceAccess, never guessed
// by pairing an arbitrary DID with whatever session happens to be tracked
// for it.
func (s *Store) candidatesForSpace(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) ([]Session, error) {
	var access []spaceAccess
	if err := s.db.WithContext(ctx).
		Where("space = ?", space).
		Find(&access).Error; err != nil {
		return nil, fmt.Errorf("load space access: %w", err)
	}
	candidates := make([]Session, len(access))
	for i, a := range access {
		candidates[i] = Session{DID: a.DID, SessionID: a.SessionID}
	}
	return candidates, nil
}

// fetchDelegationToken calls getDelegationToken for space using client,
// which must already authenticate its own requests (a resumed session's
// APIClient).
func fetchDelegationToken(
	ctx context.Context,
	client *atclient.APIClient,
	space habitat_syntax.SpaceURI,
) (string, error) {
	var out habitat.NetworkHabitatSpaceGetDelegationTokenOutput
	if err := client.Get(ctx, "network.habitat.space.getDelegationToken",
		map[string]any{"space": space.String()}, &out); err != nil {
		return "", fmt.Errorf("get delegation token: %w", err)
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

// DropSpace forgets all access records for a deleted space.
func (s *Store) DropSpace(ctx context.Context, space habitat_syntax.SpaceURI) error {
	return s.db.WithContext(ctx).
		Where("space = ?", space).
		Delete(&spaceAccess{}).Error
}
