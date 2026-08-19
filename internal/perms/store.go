package perms

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/openfga/pkg/tuple"
	"gorm.io/gorm"
)

var fgaRelationFromRole = map[habitat_syntax.SpaceRole]string{
	habitat_syntax.SpaceRoleOwner:   fgastore.RelationSpaceOwner,
	habitat_syntax.SpaceRoleManager: fgastore.RelationSpaceMemberManager,
	habitat_syntax.SpaceRoleWriter:  fgastore.RelationSpaceWriter,
	habitat_syntax.SpaceRoleReader:  fgastore.RelationSpaceReader,
}

type Store interface {
	// Additions
	// Adds a user relation (collection = network.habitat.relationship.userRelation) and returns
	// the record uri for the corresponding relationship record.
	AddUserRelation(
		ctx context.Context,
		did syntax.DID,
		space habitat_syntax.SpaceURI,
		role habitat_syntax.SpaceRole,
	) (habitat_syntax.SpaceRecordURI, error)
	// Adds a space relation (collection = network.habitat.relationship.spaceRelation) and returns
	// the record uri for the corresponding relationship record.
	AddSpaceRoleRelation(
		ctx context.Context,
		subject habitat_syntax.SpaceURI,
		subjectRole habitat_syntax.SpaceRole,
		object habitat_syntax.SpaceURI,
		objectRole habitat_syntax.SpaceRole,
	) (habitat_syntax.SpaceRecordURI, error)

	// Revocations
	RevokeUserRelation(
		ctx context.Context,
		did syntax.DID,
		space habitat_syntax.SpaceURI,
		role habitat_syntax.SpaceRole,
	) error
	RevokeSpaceRoleRelation(
		ctx context.Context,
		subjectSpace habitat_syntax.SpaceURI,
		subjectRole habitat_syntax.SpaceRole,
		objectSpace habitat_syntax.SpaceURI,
		objectRole habitat_syntax.SpaceRole,
	) error
	UnsafeRevokeAllSpaceRoles(ctx context.Context, space habitat_syntax.SpaceURI) error

	// Permission checks
	CheckUserHasSpaceRole(
		ctx context.Context,
		did syntax.DID,
		space habitat_syntax.SpaceURI,
		role habitat_syntax.SpaceRole,
	) (bool, error)
	CheckSpaceRelationHasSpaceRole(
		ctx context.Context,
		subjectSpace habitat_syntax.SpaceURI,
		subjectRole habitat_syntax.SpaceRole,
		objectSpace habitat_syntax.SpaceURI,
		objectRole habitat_syntax.SpaceRole,
	) (bool, error)

	// List various things using relations
	ListUserSubjects(ctx context.Context, space habitat_syntax.SpaceURI) ([]syntax.DID, error)
	ListObjects(ctx context.Context, did syntax.DID) ([]habitat_syntax.SpaceURI, error)
}

type TxRunner interface {
	Transaction(fc func(tx *gorm.DB) error, opts ...*sql.TxOptions) (err error)
}

type store struct {
	db     TxRunner
	fga    fgastore.Store
	spaces spaces.Store
}

func NewStore(db TxRunner, spaces spaces.Store, fga fgastore.Store) *store {
	return &store{db: db, spaces: spaces, fga: fga}
}

var _ Store = &store{}

// AddUserRelation implements [Store].
func (s *store) AddUserRelation(
	ctx context.Context,
	did syntax.DID,
	space habitat_syntax.SpaceURI,
	role habitat_syntax.SpaceRole,
) (habitat_syntax.SpaceRecordURI, error) {

	var uri habitat_syntax.SpaceRecordURI
	err := s.db.Transaction(func(tx *gorm.DB) error {
		record := map[string]any{
			"subject":   did.String(),
			"relation":  string(role),
			"createdAt": time.Now().UTC().Format(time.RFC3339),
			/* object is the space being written into itself */
		}
		var err error
		uri, _, err = s.spaces.WithTx(tx).PutRecord(ctx /* TODO should i use tx.Statement.Context */, space, space.SpaceOwner(), habitat_syntax.UserRelationCollection, "" /* rkey should be generated */, record)
		if err != nil {
			return fmt.Errorf("err putting relationship record: %w", err)
		}

		err = s.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
			Writes: &openfgav1.WriteRequestWrites{
				TupleKeys: []*openfgav1.TupleKey{
					tuple.NewTupleKey(
						fgastore.SpaceObjectKey(space),
						fgaRelationFromRole[role],
						fgastore.MemberUserString(did),
					),
				},
				OnDuplicate: "ignore",
			},
		})
		if err != nil {
			return fmt.Errorf("err writing to fga: %w", err)
		}
		return nil
	})

	return uri, err
}

// Add.SpaceRoleRelation implements [Store]. It grants every subject holding
// subjectRole on subject (a userset) objectRole on object — this is what
// powers groups-as-spaces and cross-space role inheritance.
func (s *store) AddSpaceRoleRelation(
	ctx context.Context,
	subject habitat_syntax.SpaceURI,
	subjectRole habitat_syntax.SpaceRole,
	object habitat_syntax.SpaceURI,
	objectRole habitat_syntax.SpaceRole,
) (habitat_syntax.SpaceRecordURI, error) {

	var uri habitat_syntax.SpaceRecordURI
	err := s.db.Transaction(func(tx *gorm.DB) error {
		record := map[string]any{
			"subject":     subject.String(),
			"subjectRole": string(subjectRole),
			"relation":    string(objectRole),
			"createdAt":   time.Now().UTC().Format(time.RFC3339),
			/* object is the space being written into itself */
		}
		var err error
		uri, _, err = s.spaces.WithTx(tx).PutRecord(ctx /* TODO should i use tx.Statement.Context */, object, object.SpaceOwner(), habitat_syntax.SpaceRelationCollection, "" /* rkey should be generated */, record)
		if err != nil {
			return fmt.Errorf("err putting relationship record: %w", err)
		}

		err = s.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
			Writes: &openfgav1.WriteRequestWrites{
				TupleKeys: []*openfgav1.TupleKey{
					tuple.NewTupleKey(
						fgastore.SpaceObjectKey(object),
						fgaRelationFromRole[objectRole],
						fgastore.SpaceUsersetString(subject, fgaRelationFromRole[subjectRole]),
					),
				},
				OnDuplicate: "ignore",
			},
		})
		if err != nil {
			return fmt.Errorf("err writing to fga: %w", err)
		}
		return nil
	})

	return uri, err
}

// RevokeUserRelation implements [Store].
func (s *store) RevokeUserRelation(
	ctx context.Context,
	did syntax.DID,
	space habitat_syntax.SpaceURI,
	role habitat_syntax.SpaceRole,
) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		collection := syntax.NSID(habitat_syntax.UserRelationCollection)
		records, err := s.spaces.WithTx(tx).ListRecords(ctx, space, space.SpaceOwner(), &collection)
		if err != nil {
			return fmt.Errorf("err listing relationship records: %w", err)
		}

		for _, record := range records {
			subject, _ := record.Value["subject"].(string)
			relation, _ := record.Value["relation"].(string)
			if subject != did.String() || relation != string(role) {
				continue
			}
			if err := s.spaces.WithTx(tx).DeleteRecord(ctx, space, space.SpaceOwner(), collection, record.Rkey.String()); err != nil {
				return fmt.Errorf("err deleting relationship record: %w", err)
			}
		}

		err = s.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
			Deletes: &openfgav1.WriteRequestDeletes{
				TupleKeys: []*openfgav1.TupleKeyWithoutCondition{
					tuple.TupleKeyToTupleKeyWithoutCondition(tuple.NewTupleKey(
						fgastore.SpaceObjectKey(space),
						fgaRelationFromRole[role],
						fgastore.MemberUserString(did),
					)),
				},
				OnMissing: "ignore",
			},
		})
		if err != nil {
			return fmt.Errorf("err removing from fga: %w", err)
		}
		return nil
	})
}

// Revoke.SpaceRoleRelation implements [Store].
func (s *store) RevokeSpaceRoleRelation(
	ctx context.Context,
	subjectSpace habitat_syntax.SpaceURI,
	subjectRole habitat_syntax.SpaceRole,
	objectSpace habitat_syntax.SpaceURI,
	objectRole habitat_syntax.SpaceRole,
) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		collection := syntax.NSID(habitat_syntax.SpaceRelationCollection)
		records, err := s.spaces.WithTx(tx).ListRecords(ctx, objectSpace, objectSpace.SpaceOwner(), &collection)
		if err != nil {
			return fmt.Errorf("err listing relationship records: %w", err)
		}

		for _, record := range records {
			subject, _ := record.Value["subject"].(string)
			role, _ := record.Value["subjectRole"].(string)
			relation, _ := record.Value["relation"].(string)
			if subject != subjectSpace.String() || role != string(subjectRole) || relation != string(objectRole) {
				continue
			}
			if err := s.spaces.WithTx(tx).DeleteRecord(ctx, objectSpace, objectSpace.SpaceOwner(), collection, record.Rkey.String()); err != nil {
				return fmt.Errorf("err deleting relationship record: %w", err)
			}
		}

		err = s.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
			Deletes: &openfgav1.WriteRequestDeletes{
				TupleKeys: []*openfgav1.TupleKeyWithoutCondition{
					tuple.TupleKeyToTupleKeyWithoutCondition(tuple.NewTupleKey(
						fgastore.SpaceObjectKey(objectSpace),
						fgaRelationFromRole[objectRole],
						fgastore.SpaceUsersetString(subjectSpace, fgaRelationFromRole[subjectRole]),
					)),
				},
				OnMissing: "ignore",
			},
		})
		if err != nil {
			return fmt.Errorf("err writing to fga: %w", err)
		}
		return nil
	})
}

// UnsafeRevokeAllhabitat_syntax.SpaceRoles implements [Store]. It reads back every tuple stored
// against space, so it doesn't need to know which roles/subjects exist, then
// deletes them all, along with every relationship record persisted into the
// space — for use when a space is deleted.
//
// This is only meant to be used upon space deletion--if this is called on a space that is
// still used, it will be in a broken state.
func (s *store) UnsafeRevokeAllSpaceRoles(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) error {
	tuples, err := s.fga.Read(ctx, fgastore.Tuple{Object: fgastore.SpaceObjectKey(space)})
	if err != nil {
		return err
	}

	return s.db.Transaction(func(tx *gorm.DB) error {
		for _, collectionName := range []string{habitat_syntax.UserRelationCollection, habitat_syntax.SpaceRelationCollection} {
			collection := syntax.NSID(collectionName)
			records, err := s.spaces.WithTx(tx).ListRecords(ctx, space, space.SpaceOwner(), &collection)
			if err != nil {
				return fmt.Errorf("err listing relationship records: %w", err)
			}
			for _, record := range records {
				if err := s.spaces.WithTx(tx).DeleteRecord(ctx, space, space.SpaceOwner(), collection, record.Rkey.String()); err != nil {
					return fmt.Errorf("err deleting relationship record: %w", err)
				}
			}
		}

		if len(tuples) == 0 {
			return nil
		}

		deletes := make([]*openfgav1.TupleKeyWithoutCondition, len(tuples))
		for i, t := range tuples {
			deletes[i] = tuple.TupleKeyToTupleKeyWithoutCondition(
				tuple.NewTupleKey(t.Object, t.Relation, t.User),
			)
		}
		err = s.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
			Deletes: &openfgav1.WriteRequestDeletes{
				TupleKeys: deletes,
				OnMissing: "ignore",
			},
		})
		if err != nil {
			return fmt.Errorf("err removing from fga: %w", err)
		}
		return nil
	})
}

// CheckUserHashabitat_syntax.SpaceRole implements [Store]. The space's owner is treated as
// an implicit habitat_syntax.SpaceRoleOwner, and the owning org's members as implicit
// habitat_syntax.SpaceRoleReaders, without either needing a stored tuple.
func (s *store) CheckUserHasSpaceRole(
	ctx context.Context,
	did syntax.DID,
	space habitat_syntax.SpaceURI,
	role habitat_syntax.SpaceRole,
) (bool, error) {
	return s.fga.Check(
		ctx,
		fgastore.MemberUserString(did),
		fgaRelationFromRole[role],
		fgastore.SpaceObjectKey(space),
		fgastore.OwnerContextualTuple(space),
		fgastore.OrgMemberContextualTuple(space.SpaceOwner()),
	)
}

// CheckSpaceRelationHashabitat_syntax.SpaceRole implements [Store]: whether every subject
// holding subjectRole on subjectSpace also holds objectRole on objectSpace.
func (s *store) CheckSpaceRelationHasSpaceRole(
	ctx context.Context,
	subjectSpace habitat_syntax.SpaceURI,
	subjectRole habitat_syntax.SpaceRole,
	objectSpace habitat_syntax.SpaceURI,
	objectRole habitat_syntax.SpaceRole,
) (bool, error) {
	return s.fga.Check(
		ctx,
		fgastore.SpaceUsersetString(subjectSpace, fgaRelationFromRole[subjectRole]),
		fgaRelationFromRole[objectRole],
		fgastore.SpaceObjectKey(objectSpace),
		fgastore.OwnerContextualTuple(objectSpace),
		fgastore.OrgMemberContextualTuple(objectSpace.SpaceOwner()),
	)
}

// ListUserSubjects implements [Store]. Unlike ListSubjects, this expands
// implicit grantees (the space's owner, and members of the owning org) via
// contextual tuples, since callers want the full set of users who can read
// the space rather than just its stored tuples.
func (s *store) ListUserSubjects(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) ([]syntax.DID, error) {
	users, err := s.fga.ListUsers(
		ctx,
		fgastore.SpaceObjectKey(space),
		fgastore.RelationSpaceReader,
		fgastore.OwnerContextualTuple(space),
		fgastore.OrgMemberContextualTuple(space.SpaceOwner()),
	)
	if err != nil {
		return nil, fmt.Errorf("perms: list user subjects: %w", err)
	}

	dids := make([]syntax.DID, 0, len(users))
	for _, user := range users {
		did, err := fgastore.MemberUserToDID(user)
		if err != nil {
			return nil, fmt.Errorf("perms: list user subjects: %w", err)
		}
		dids = append(dids, did)
	}
	return dids, nil
}

// ListObjects implements [Store]. It returns the spaces did directly holds
// at least habitat_syntax.SpaceRoleReader on (reader is implied by every other role, so
// this covers manager/writer/owner too). Owner/org-member implications
// aren't expanded here, since that would require checking every space in
// every org did could be a member of.
func (s *store) ListObjects(
	ctx context.Context,
	did syntax.DID,
) ([]habitat_syntax.SpaceURI, error) {
	keys, err := s.fga.ListObjects(
		ctx,
		fgastore.MemberUserString(did),
		fgaRelationFromRole[habitat_syntax.SpaceRoleReader],
		fgastore.TypeSpace,
	)
	if err != nil {
		return nil, err
	}

	spaceURIs := make([]habitat_syntax.SpaceURI, 0, len(keys))
	for _, key := range keys {
		uri, err := fgastore.ParseSpaceObjectKey(key)
		if err != nil {
			return nil, fmt.Errorf("perms: list objects: %w", err)
		}
		spaceURIs = append(spaceURIs, uri)
	}
	return spaceURIs, nil
}
