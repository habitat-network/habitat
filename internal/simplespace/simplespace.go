package simplespace

import (
	"context"
	"errors"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/openfga/pkg/tuple"
	"gorm.io/gorm"
)

type space struct {
	Owner     syntax.DID              `gorm:"primaryKey"`
	Type      syntax.NSID             `gorm:"primaryKey"`
	Skey      habitat_syntax.SpaceKey `gorm:"primaryKey"`
	CreatedAt time.Time
}

// Methods are analagous to those defined here: https://github.com/bluesky-social/proposals/tree/main/0016-permissioned-data#required-pds-space-management-simplespace
type Manager interface {
	// Space operations
	CreateSpace(
		ctx context.Context,
		org syntax.DID,
		creator syntax.DID,
		spaceType syntax.NSID,
		skey habitat_syntax.SpaceKey,
	) (habitat_syntax.SpaceURI, error)
	DeleteSpace(ctx context.Context, uri habitat_syntax.SpaceURI) error

	// Member operations
	AddMember(ctx context.Context, space habitat_syntax.SpaceURI, did syntax.DID) error
	RemoveMember(ctx context.Context, space habitat_syntax.SpaceURI, did syntax.DID) error
	ListMembers(ctx context.Context, callerOrg syntax.DID /* todo: remove this when org membership uses spaces*/, space habitat_syntax.SpaceURI) ([]syntax.DID, error)
}

type manager struct {
	db       *gorm.DB
	spaces   spaces.Store
	clock    *syntax.TIDClock
	fga      fgastore.Store
	notifier spaces.Notifier
}

var _ Manager = &manager{}

// WithTx implements [Manager], returning a manager whose DB operations run on tx.
func (m *manager) WithTx(tx *gorm.DB) Manager {
	return &manager{
		db:     tx,
		spaces: m.spaces,
		fga:    m.fga,
		clock:  syntax.NewTIDClock(0 /* TODO: should this be a random number */),
	}
}

var (
	ErrCannotRemoveOrg    = errors.New("cannot remove the org from the space")
	ErrSpaceNotFound      = errors.New("space not found")
	ErrSpaceAlreadyExists = errors.New("space already exists")
)

// CreateSpace implements [Manager].
func (m *manager) CreateSpace(ctx context.Context, org syntax.DID, creator syntax.DID, spaceType syntax.NSID, skey habitat_syntax.SpaceKey) (habitat_syntax.SpaceURI, error) {
	if skey == "" {
		// TODO: should this / does this need to be a TID?
		skey = habitat_syntax.NewSkey(m.clock.Next())
	}

	err := m.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&space{
			Owner: org,
			Type:  spaceType,
			Skey:  skey,
		}).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return ErrSpaceAlreadyExists
			}
			return err
		}

		return m.fga.Write(
			ctx,
			fgastore.MemberUserString(creator),
			fgastore.RelationSpaceOwner,
			fgastore.SpaceObjectKey(habitat_syntax.ConstructSpaceURI(org, spaceType, skey)),
		)
	})
	if err != nil {
		return "", err
	}

	return habitat_syntax.ConstructSpaceURI(org, spaceType, skey), nil
}

// DeleteSpace implements [Manager].
func (m *manager) DeleteSpace(ctx context.Context, uri habitat_syntax.SpaceURI) error {
	// read the stored FGA tuples for this space before deleting anything,
	// so we know exactly what tuples to delete
	tuples, err := m.fga.Read(ctx, fgastore.Tuple{Object: fgastore.SpaceObjectKey(uri)})
	if err != nil {
		return err
	}

	// everything after this point is idempotent — use a transaction
	err = m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := m.spaces.WithTx(tx).DeleteSpaceRepos(ctx, uri)
		if err != nil {
			return err
		}

		deleteSpace := tx.
			Where("owner = ? AND skey = ?", uri.SpaceOwner(), uri.Skey()).
			Delete(&space{})
		if deleteSpace.Error != nil {
			return err
		}
		if deleteSpace.RowsAffected == 0 {
			return ErrSpaceNotFound
		}

		// delete all stored FGA tuples for this space
		var deletes []*openfgav1.TupleKeyWithoutCondition
		for _, t := range tuples {
			deletes = append(deletes, tuple.TupleKeyToTupleKeyWithoutCondition(
				tuple.NewTupleKey(t.Object, t.Relation, t.User),
			))
		}
		if len(deletes) > 0 {
			return m.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
				Deletes: &openfgav1.WriteRequestDeletes{
					TupleKeys: deletes,
					OnMissing: "ignore",
				},
			})
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Best-effort: tell registered syncers the space is gone.
	m.notifier.NotifySpaceDeleted(ctx, uri)
	return nil
}

// ListMembers implements [Manager].
func (m *manager) ListMembers(ctx context.Context, callerOrg syntax.DID, space habitat_syntax.SpaceURI) ([]syntax.DID, error) {
	users, err := m.fga.ListUsers(
		ctx,
		fgastore.SpaceObjectKey(space),
		fgastore.RelationSpaceReader,
		fgastore.OwnerContextualTuple(space),
		fgastore.OrgMemberContextualTuple(callerOrg),
	)
	if err != nil {
		return nil, err
	}

	dids := make([]syntax.DID, len(users))
	for i, user := range users {
		did, err := fgastore.MemberUserToDID(user)
		if err != nil {
			return nil, err
		}
		dids[i] = did
	}
	return dids, nil
}

// AddMember implements [Manager].
func (m *manager) AddMember(ctx context.Context, uri habitat_syntax.SpaceURI, did syntax.DID) error {
	var sp space
	err := m.db.WithContext(ctx).
		Where("owner = ? AND skey = ?", uri.SpaceOwner(), uri.Skey()).
		First(&sp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrSpaceNotFound
	} else if err != nil {
		return err
	}
	if did == uri.SpaceOwner() {
		return nil
	}
	return m.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
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
}

// RemoveMember implements [Manager].
func (m *manager) RemoveMember(ctx context.Context, uri habitat_syntax.SpaceURI, did syntax.DID) error {
	if did == uri.SpaceOwner() {
		return ErrCannotRemoveOrg
	}

	var sp space
	err := m.db.WithContext(ctx).
		Where("owner = ? AND skey = ?", uri.SpaceOwner(), uri.Skey()).
		First(&sp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrSpaceNotFound
	} else if err != nil {
		return err
	}

	return m.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
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
}

func (m *manager) IsMember(
	ctx context.Context,
	org syntax.DID,
	uri habitat_syntax.SpaceURI,
	did syntax.DID,
) (bool, error) {
	return m.fga.Check(
		ctx,
		fgastore.MemberUserString(did),
		fgastore.RelationSpaceReader,
		fgastore.SpaceObjectKey(uri),
		fgastore.OwnerContextualTuple(uri),
		fgastore.OrgMemberContextualTuple(org),
	)
}
