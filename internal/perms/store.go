package perms

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/fgastore"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/openfga/pkg/tuple"
)

// SpaceRole is an access-control role held on a space. The hierarchy
// (owner ⇒ manager ⇒ writer ⇒ reader) is enforced by the OpenFGA model
// (internal/fgastore's authModel), not by this package.
type SpaceRole string

const (
	SpaceRoleOwner   SpaceRole = "owner"
	SpaceRoleManager SpaceRole = "manager"
	SpaceRoleWriter  SpaceRole = "writer"
	SpaceRoleReader  SpaceRole = "reader"
)

var fgaRelationFromRole = map[SpaceRole]string{
	SpaceRoleOwner:   fgastore.RelationSpaceOwner,
	SpaceRoleManager: fgastore.RelationSpaceMemberManager,
	SpaceRoleWriter:  fgastore.RelationSpaceWriter,
	SpaceRoleReader:  fgastore.RelationSpaceReader,
}

type SpaceRoleSubject struct {
	Space habitat_syntax.SpaceURI
	Role  SpaceRole
}

type Store interface {
	// Additions
	AddUserRelation(
		ctx context.Context,
		did syntax.DID,
		space habitat_syntax.SpaceURI,
		role SpaceRole,
	) error
	AddSpaceRoleRelation(
		ctx context.Context,
		subject habitat_syntax.SpaceURI,
		subjectRole SpaceRole,
		object habitat_syntax.SpaceURI,
		objectRole SpaceRole,
	) error

	// Revocations
	RevokeUserRelation(
		ctx context.Context,
		did syntax.DID,
		space habitat_syntax.SpaceURI,
		role SpaceRole,
	) error
	RevokeAllUserRelations(
		ctx context.Context,
		did syntax.DID,
		space habitat_syntax.SpaceURI,
	) error
	RevokeSpaceRoleRelation(
		ctx context.Context,
		subjectSpace habitat_syntax.SpaceURI,
		subjectRole SpaceRole,
		objectSpace habitat_syntax.SpaceURI,
		objectRole SpaceRole,
	) error
	UnsafeRevokeAllSpaceRoles(ctx context.Context, space habitat_syntax.SpaceURI) error

	// Permission checks
	CheckUserHasSpaceRole(
		ctx context.Context,
		did syntax.DID,
		space habitat_syntax.SpaceURI,
		role SpaceRole,
	) (bool, error)
	CheckSpaceRelationHasSpaceRole(
		ctx context.Context,
		subjectSpace habitat_syntax.SpaceURI,
		subjectRole SpaceRole,
		objectSpace habitat_syntax.SpaceURI,
		objectRole SpaceRole,
	) (bool, error)

	// List various things using relations
	ListUserSubjects(ctx context.Context, space habitat_syntax.SpaceURI) ([]syntax.DID, error)
	ListObjects(ctx context.Context, did syntax.DID) ([]habitat_syntax.SpaceURI, error)
}

type store struct {
	fga fgastore.Store
}

func NewStore(fga fgastore.Store) Store {
	return &store{fga: fga}
}

var _ Store = &store{}

// AddUserRelation implements [Store].
func (s *store) AddUserRelation(
	ctx context.Context,
	did syntax.DID,
	space habitat_syntax.SpaceURI,
	role SpaceRole,
) error {
	return s.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
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
}

// AddSpaceRoleRelation implements [Store]. It grants every subject holding
// subjectRole on subject (a userset) objectRole on object — this is what
// powers groups-as-spaces and cross-space role inheritance.
func (s *store) AddSpaceRoleRelation(
	ctx context.Context,
	subject habitat_syntax.SpaceURI,
	subjectRole SpaceRole,
	object habitat_syntax.SpaceURI,
	objectRole SpaceRole,
) error {
	return s.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
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
}

// RevokeUserRelation implements [Store].
func (s *store) RevokeUserRelation(
	ctx context.Context,
	did syntax.DID,
	space habitat_syntax.SpaceURI,
	role SpaceRole,
) error {
	return s.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
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
}

// RevokeAllUserRelations implements [Store].
func (s *store) RevokeAllUserRelations(
	ctx context.Context,
	did syntax.DID,
	space habitat_syntax.SpaceURI,
) error {
	return s.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
		Deletes: &openfgav1.WriteRequestDeletes{
			TupleKeys: []*openfgav1.TupleKeyWithoutCondition{
				tuple.TupleKeyToTupleKeyWithoutCondition(tuple.NewTupleKey(
					fgastore.SpaceObjectKey(space),
					fgastore.RelationSpaceOwner,
					fgastore.MemberUserString(did),
				)),
				tuple.TupleKeyToTupleKeyWithoutCondition(tuple.NewTupleKey(
					fgastore.SpaceObjectKey(space),
					fgastore.RelationSpaceMemberManager,
					fgastore.MemberUserString(did),
				)),
				tuple.TupleKeyToTupleKeyWithoutCondition(tuple.NewTupleKey(
					fgastore.SpaceObjectKey(space),
					fgastore.RelationSpaceWriter,
					fgastore.MemberUserString(did),
				)),
				tuple.TupleKeyToTupleKeyWithoutCondition(tuple.NewTupleKey(
					fgastore.SpaceObjectKey(space),
					fgastore.RelationSpaceReader,
					fgastore.MemberUserString(did),
				)),
			},
			OnMissing: "ignore",
		},
	})
}

// RevokeSpaceRoleRelation implements [Store].
func (s *store) RevokeSpaceRoleRelation(
	ctx context.Context,
	subjectSpace habitat_syntax.SpaceURI,
	subjectRole SpaceRole,
	objectSpace habitat_syntax.SpaceURI,
	objectRole SpaceRole,
) error {
	return s.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
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
}

// UnsafeRevokeAllSpaceRoles implements [Store]. It reads back every tuple stored
// against space, so it doesn't need to know which roles/subjects exist, then
// deletes them all — for use when a space is deleted.
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
	if len(tuples) == 0 {
		return nil
	}

	deletes := make([]*openfgav1.TupleKeyWithoutCondition, len(tuples))
	for i, t := range tuples {
		deletes[i] = tuple.TupleKeyToTupleKeyWithoutCondition(
			tuple.NewTupleKey(t.Object, t.Relation, t.User),
		)
	}
	return s.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
		Deletes: &openfgav1.WriteRequestDeletes{
			TupleKeys: deletes,
			OnMissing: "ignore",
		},
	})
}

// CheckUserHasSpaceRole implements [Store]. The space's owner is treated as
// an implicit SpaceRoleOwner, and the owning org's members as implicit
// SpaceRoleReaders, without either needing a stored tuple.
func (s *store) CheckUserHasSpaceRole(
	ctx context.Context,
	did syntax.DID,
	space habitat_syntax.SpaceURI,
	role SpaceRole,
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

// CheckSpaceRelationHasSpaceRole implements [Store]: whether every subject
// holding subjectRole on subjectSpace also holds objectRole on objectSpace.
func (s *store) CheckSpaceRelationHasSpaceRole(
	ctx context.Context,
	subjectSpace habitat_syntax.SpaceURI,
	subjectRole SpaceRole,
	objectSpace habitat_syntax.SpaceURI,
	objectRole SpaceRole,
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
// at least SpaceRoleReader on (reader is implied by every other role, so
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
		fgaRelationFromRole[SpaceRoleReader],
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
