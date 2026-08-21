package simplespace

import (
	"context"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/perms"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"gorm.io/gorm"
)

type Store struct {
	db     *gorm.DB
	spaces spaces.Store
	perms  perms.Store

	clock *syntax.TIDClock
}

var (
	ErrCannotRemoveOrg    = errors.New("cannot remove the org from the space")
	ErrSpaceAlreadyExists = errors.New("space already exists")
)

func NewStore(
	db *gorm.DB,
	spaces spaces.Store,
	perms perms.Store,
) *Store {
	return &Store{
		db:     db,
		spaces: spaces,
		clock:  syntax.NewTIDClock(0),
		perms:  perms,
	}
}

// WithTx implements [Store], returning a manager whose DB operations run on tx.
func (m *Store) WithTx(tx *gorm.DB) *Store {
	return &Store{
		db:     tx,
		spaces: m.spaces,
		perms:  m.perms,
		clock:  m.clock,
	}
}

// CreateSpace implements [Store].
func (m *Store) CreateSpace(
	ctx context.Context,
	org syntax.DID,
	creator syntax.DID,
	spaceType syntax.NSID,
	skey habitat_syntax.SpaceKey,
) (habitat_syntax.SpaceURI, error) {

	var uri habitat_syntax.SpaceURI
	err := m.db.Transaction(func(tx *gorm.DB) error {
		var err error
		uri, err = m.spaces.WithTx(tx).CreateSpace(ctx, org, creator, spaceType, skey)
		if errors.Is(err, spaces.ErrSpaceAlreadyExists) {
			return ErrSpaceAlreadyExists
		} else if err != nil {
			return fmt.Errorf("err creating space: %w", err)
		}

		_, err = m.perms.SetUserRelation(ctx, creator, uri, habitat_syntax.SpaceRoleOwner)
		if err != nil {
			return fmt.Errorf("err writing permissions: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("err creating space: %w", err)
	}

	return uri, nil
}

// DeleteSpace implements [Store].
func (m *Store) DeleteSpace(ctx context.Context, uri habitat_syntax.SpaceURI) error {
	// everything after this point is idempotent — use a transaction
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := m.spaces.WithTx(tx).DeleteSpace(ctx, uri)
		if err != nil {
			return fmt.Errorf("err deleting space: %w", err)
		}

		// delete all stored relations/permissions for this space
		if err := m.perms.UnsafeRevokeAllSpaceRoles(ctx, uri); err != nil {
			return fmt.Errorf("err deleting space permissions: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("err deleting space: %w", err)
	}

	return nil
}

// ListMembers implements [Store].
func (m *Store) ListMembers(
	ctx context.Context,
	callerOrg syntax.DID,
	space habitat_syntax.SpaceURI,
) ([]syntax.DID, error) {
	dids, err := m.perms.ListUserSubjects(ctx, space)
	if err != nil {
		return nil, fmt.Errorf("err listing users: %w", err)
	}
	return dids, nil
}

// AddMember implements [Store].
func (m *Store) AddMember(ctx context.Context, uri habitat_syntax.SpaceURI, did syntax.DID) error {
	ok, err := m.spaces.CheckSpaceExists(ctx, uri)
	if err != nil {
		return fmt.Errorf("err checking space existence: %w", err)
	} else if !ok {
		return spaces.ErrSpaceNotFound
	}

	// TODO: there could be a race here between TOCTOU -- the space could get deleted by another process here.
	// We need a way to detect this after the fact and clean it up.

	if _, err := m.perms.SetUserRelation(
		ctx,
		did,
		uri,
		habitat_syntax.SpaceRoleWriter,
	); err != nil {
		return fmt.Errorf("err adding member: %w", err)
	}
	return nil
}

// RemoveMember implements [Store].
// Semantics of RemoveMember is that the user is no longer part of the space, not in any role.
func (m *Store) RemoveMember(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	did syntax.DID,
) error {
	if did == uri.SpaceOwner() {
		return ErrCannotRemoveOrg
	}

	ok, err := m.spaces.CheckSpaceExists(ctx, uri)
	if err != nil {
		return fmt.Errorf("err checking space existence: %w", err)
	} else if !ok {
		return spaces.ErrSpaceNotFound
	}

	if err := m.perms.RevokeUser(ctx, did, uri); err != nil {
		return fmt.Errorf("err removing member: %w", err)
	}
	return nil
}

func (m *Store) IsMember(
	ctx context.Context,
	org syntax.DID,
	uri habitat_syntax.SpaceURI,
	did syntax.DID,
) (bool, error) {
	ok, err := m.perms.CheckUserHasSpaceRole(ctx, did, uri, habitat_syntax.SpaceRoleReader)
	if err != nil {
		return false, fmt.Errorf("err checking membership: %w", err)
	}
	return ok, nil
}
