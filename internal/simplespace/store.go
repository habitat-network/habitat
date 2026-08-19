package simplespace

import (
	"context"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/openfga/pkg/tuple"
	"gorm.io/gorm"
)

type Store struct {
	db     *gorm.DB
	spaces spaces.Store

	clock *syntax.TIDClock
	fga   fgastore.Store
}

var (
	ErrCannotRemoveOrg    = errors.New("cannot remove the org from the space")
	ErrSpaceAlreadyExists = errors.New("space already exists")
)

func NewStore(
	db *gorm.DB,
	spaces spaces.Store,
	fga fgastore.Store,
) *Store {
	return &Store{
		db:     db,
		spaces: spaces,
		clock:  syntax.NewTIDClock(0),
		fga:    fga,
	}
}

// WithTx implements [Store], returning a manager whose DB operations run on tx.
func (m *Store) WithTx(tx *gorm.DB) *Store {
	return &Store{
		db:     tx,
		spaces: m.spaces,
		fga:    m.fga,
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

		err = m.fga.Write(
			ctx,
			fgastore.MemberUserString(creator),
			fgastore.RelationSpaceOwner,
			fgastore.SpaceObjectKey(uri),
		)
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
	// read the stored FGA tuples for this space before deleting anything,
	// so we know exactly what tuples to delete
	tuples, err := m.fga.Read(ctx, fgastore.Tuple{Object: fgastore.SpaceObjectKey(uri)})
	if err != nil {
		return fmt.Errorf("err reading space permissions: %w", err)
	}

	// everything after this point is idempotent — use a transaction
	err = m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := m.spaces.WithTx(tx).DeleteSpace(ctx, uri)
		if err != nil {
			return fmt.Errorf("err deleting space: %w", err)
		}

		// delete all stored FGA tuples for this space
		var deletes []*openfgav1.TupleKeyWithoutCondition
		for _, t := range tuples {
			deletes = append(deletes, tuple.TupleKeyToTupleKeyWithoutCondition(
				tuple.NewTupleKey(t.Object, t.Relation, t.User),
			))
		}
		if len(deletes) == 0 {
			return nil
		}
		err = m.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
			Deletes: &openfgav1.WriteRequestDeletes{
				TupleKeys: deletes,
				OnMissing: "ignore",
			},
		})
		if err != nil {
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
	users, err := m.fga.ListUsers(
		ctx,
		fgastore.SpaceObjectKey(space),
		fgastore.RelationSpaceReader,
		fgastore.OwnerContextualTuple(space),
		fgastore.OrgMemberContextualTuple(callerOrg),
	)
	if err != nil {
		return nil, fmt.Errorf("err listing users: %w", err)
	}

	dids := make([]syntax.DID, len(users))
	for i, user := range users {
		did, err := fgastore.MemberUserToDID(user)
		if err != nil {
			return nil, fmt.Errorf("err listing users, member to did: %w", err)
		}
		dids[i] = did
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

	err = m.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
		Writes: &openfgav1.WriteRequestWrites{
			TupleKeys: []*openfgav1.TupleKey{
				tuple.NewTupleKey(
					fgastore.SpaceObjectKey(uri),
					fgastore.RelationSpaceWriter,
					fgastore.MemberUserString(did),
				),
			},
			OnDuplicate: "ignore",
		},
		Deletes: &openfgav1.WriteRequestDeletes{
			TupleKeys: []*openfgav1.TupleKeyWithoutCondition{
				tuple.TupleKeyToTupleKeyWithoutCondition(tuple.NewTupleKey(
					fgastore.SpaceObjectKey(uri),
					fgastore.RelationSpaceReader,
					fgastore.MemberUserString(did),
				)),
			},
			OnMissing: "ignore",
		},
	})
	if err != nil {
		return fmt.Errorf("err adding member: %w", err)
	}
	return nil
}

// RemoveMember implements [Store].
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

	err = m.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
		Deletes: &openfgav1.WriteRequestDeletes{
			TupleKeys: []*openfgav1.TupleKeyWithoutCondition{
				tuple.TupleKeyToTupleKeyWithoutCondition(tuple.NewTupleKey(
					fgastore.SpaceObjectKey(uri),
					fgastore.RelationSpaceReader,
					fgastore.MemberUserString(did),
				)),
				tuple.TupleKeyToTupleKeyWithoutCondition(tuple.NewTupleKey(
					fgastore.SpaceObjectKey(uri),
					fgastore.RelationSpaceWriter,
					fgastore.MemberUserString(did),
				)),
			},
			OnMissing: "ignore",
		},
	})
	if err != nil {
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
	ok, err := m.fga.Check(
		ctx,
		fgastore.MemberUserString(did),
		fgastore.RelationSpaceReader,
		fgastore.SpaceObjectKey(uri),
		fgastore.OwnerContextualTuple(uri),
		fgastore.OrgMemberContextualTuple(org),
	)
	if err != nil {
		return false, fmt.Errorf("err checking membership: %w", err)
	}
	return ok, nil
}
