// Package session tracks which DIDs sap syncs on behalf of, and which spaces
// each can access. It does not itself know how a DID's session was
// established (browser OAuth flow, JWT-bearer grant, ...) — that happens out
// of band, entirely the caller's concern; the caller supplies a Clients
// implementation, and this package's job is tracking *which* DIDs to ask it
// about and caching space-credential exchanges on top.
package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/habitat-network/habitat/api/habitat"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/pkg/sap/credential"
)

// Clients resolves an authenticated HTTP client for a DID sap tracks. The
// caller owns how that DID's session was actually established — sap only
// needs to be able to ask for a client on demand. Satisfied by whatever the
// caller wires up around its own session storage (e.g. an *oauth.ClientApp
// plus a DID→session-ID mapping it keeps for jwt-bearer-minted sessions).
type Clients interface {
	ClientForDID(ctx context.Context, did syntax.DID) (*http.Client, error)
}

// session is a DID sap tracks for backfill/sync, added via AddSession once
// the caller has some way to authenticate as it.
type session struct {
	DID       syntax.DID `gorm:"column:did;primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time
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

// Store tracks sessions and space access, and builds authenticated clients.
type Store struct {
	db      *gorm.DB
	clients Clients

	// mgr mints and caches space credentials, one per space regardless of
	// which session was used to obtain it — a space credential authorizes
	// the space, not the member who fetched it. Store implements
	// credential.Delegator so mgr can ask any accessing session for a
	// delegation token on demand.
	mgr *credential.Manager
}

func NewStore(db *gorm.DB, clients Clients, dir credential.Directory) *Store {
	s := &Store{db: db, clients: clients}
	s.mgr = credential.NewManager(dir, &http.Client{}, s)
	return s
}

// WithTx returns a Store scoped to the given transaction. The credential
// manager is shared, not tx-scoped: it's in-memory HTTP client state, not
// data this transaction writes.
func (s *Store) WithTx(tx *gorm.DB) *Store {
	return &Store{db: tx, clients: s.clients, mgr: s.mgr}
}

// Add starts tracking did for backfill/sync. Idempotent: safe to call again
// for a DID already tracked.
func (s *Store) Add(ctx context.Context, did syntax.DID) error {
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		DoNothing: true,
	}).Create(&session{DID: did}).Error
}

// List returns the DIDs of all tracked sessions.
func (s *Store) List(ctx context.Context) ([]syntax.DID, error) {
	var dids []syntax.DID
	if err := s.db.WithContext(ctx).
		Model(&session{}).
		Pluck("did", &dids).Error; err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return dids, nil
}

// ClientForSession returns an HTTP client authenticated as did, via the
// caller-supplied Clients.
func (s *Store) ClientForSession(ctx context.Context, did syntax.DID) (*http.Client, error) {
	return s.clients.ClientForDID(ctx, did)
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
func (s *Store) DelegationToken(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) (string, error) {
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
		http.NoBody)
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
