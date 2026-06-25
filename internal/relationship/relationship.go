// Package relationship implements the app-facing relationship-based access
// control surface: tuples and groups stored as AT Protocol records (owned by the
// org repo within the space they govern) and mirrored into the fgastore graph for
// resolution. OpenFGA is an internal detail; callers work in terms of a fixed
// role vocabulary, subjects, and objects.
package relationship

import (
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/fgastore"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// $type discriminators for the relationship subject/object unions, matching the
// network.habitat.relationship.defs lexicon.
const (
	typeUserSubject      = "network.habitat.relationship.defs#userSubject"
	typeGroupSubject     = "network.habitat.relationship.defs#groupSubject"
	typeSpaceRoleSubject = "network.habitat.relationship.defs#spaceRoleSubject"
	typeOrgRoleSubject   = "network.habitat.relationship.defs#orgRoleSubject"
	typeSpaceObject      = "network.habitat.relationship.defs#spaceObject"
	typeGroupObject      = "network.habitat.relationship.defs#groupObject"
)

var (
	// ErrInvalidTuple is returned when a (subject, relation, object) combination
	// is not representable, e.g. relation "member" on a space object.
	ErrInvalidTuple = errors.New("invalid relationship tuple")
	// ErrTupleNotFound is returned when no tuple record exists at a given URI.
	ErrTupleNotFound = errors.New("relationship tuple not found")
	// ErrGroupNotFound is returned when no group record exists at a given URI.
	ErrGroupNotFound = errors.New("relationship group not found")
)

// Role is the app-facing relation vocabulary. It hides the OpenFGA relation
// names, which are the only place the underlying engine leaks through.
type Role string

const (
	RoleOwner   Role = "owner"
	RoleManager Role = "manager"
	RoleWriter  Role = "writer"
	RoleReader  Role = "reader"
	RoleMember  Role = "member"
	// RoleAdmin applies only to org-role subjects (admin|member).
	RoleAdmin Role = "admin"
)

// spaceRoles are the roles assignable on a space.
var spaceRoles = map[Role]string{
	RoleOwner:   fgastore.RelationSpaceOwner,
	RoleManager: fgastore.RelationSpaceMemberManager,
	RoleWriter:  fgastore.RelationSpaceWriter,
	RoleReader:  fgastore.RelationSpaceReader,
}

// fgaSpaceRelation translates a space role to its FGA relation.
func (r Role) fgaSpaceRelation() (string, error) {
	rel, ok := spaceRoles[r]
	if !ok {
		return "", fmt.Errorf("%w: %q is not a space role", ErrInvalidTuple, r)
	}
	return rel, nil
}

// Subject is who a relationship grants access to.
type Subject interface{ isSubject() }

// UserSubject is an individual user identified by DID.
type UserSubject struct{ DID syntax.DID }

// GroupSubject is all members of a group (a userset).
type GroupSubject struct{ Group habitat_syntax.SpaceRecordURI }

// SpaceRoleSubject is all holders of a role on a space (a userset), enabling
// cross-space inheritance.
type SpaceRoleSubject struct {
	Space habitat_syntax.SpaceURI
	Role  Role
}

// OrgRoleSubject is all holders of a role (admin or member) in an org.
type OrgRoleSubject struct {
	Org  syntax.DID
	Role Role
}

func (UserSubject) isSubject()      {}
func (GroupSubject) isSubject()     {}
func (SpaceRoleSubject) isSubject() {}
func (OrgRoleSubject) isSubject()   {}

// Object is what a relationship is on.
type Object interface{ isObject() }

// SpaceObject is a space, with roles owner|manager|writer|reader.
type SpaceObject struct{ Space habitat_syntax.SpaceURI }

// GroupObject is a group, with the single relation "member".
type GroupObject struct{ Group habitat_syntax.SpaceRecordURI }

func (SpaceObject) isObject() {}
func (GroupObject) isObject() {}

// Tuple is a stored relationship tuple together with its record URI.
type Tuple struct {
	URI      habitat_syntax.SpaceRecordURI
	Subject  Subject
	Relation Role
	Object   Object
}

// Group is a stored group with its metadata.
type Group struct {
	URI         habitat_syntax.SpaceRecordURI
	Name        string
	Description string
	CreatedAt   string
}

// subjectToFGAUser encodes a subject as an FGA user string.
func subjectToFGAUser(s Subject) (string, error) {
	switch s := s.(type) {
	case UserSubject:
		return fgastore.MemberUserString(s.DID), nil
	case GroupSubject:
		return fgastore.GroupMemberUserString(s.Group), nil
	case SpaceRoleSubject:
		rel, err := s.Role.fgaSpaceRelation()
		if err != nil {
			return "", err
		}
		return fgastore.SpaceRoleUserString(s.Space, rel), nil
	case OrgRoleSubject:
		rel, err := orgRoleRelation(s.Role)
		if err != nil {
			return "", err
		}
		return fgastore.OrgRoleUserString(s.Org, rel), nil
	default:
		return "", fmt.Errorf("%w: unknown subject type", ErrInvalidTuple)
	}
}

// orgRoleRelation translates an org role (admin|member) to its FGA relation.
func orgRoleRelation(role Role) (string, error) {
	switch role {
	case RoleAdmin:
		return fgastore.RelationAdmin, nil
	case RoleMember:
		return fgastore.RelationMember, nil
	default:
		return "", fmt.Errorf("%w: %q is not an org role", ErrInvalidTuple, role)
	}
}

// resolveObject validates a (relation, object) pair and returns the FGA object
// key and the FGA relation to use.
func resolveObject(relation Role, object Object) (objectKey, fgaRelation string, err error) {
	switch o := object.(type) {
	case SpaceObject:
		rel, err := relation.fgaSpaceRelation()
		if err != nil {
			return "", "", err
		}
		return fgastore.SpaceObjectKey(o.Space), rel, nil
	case GroupObject:
		if relation != RoleMember {
			return "", "", fmt.Errorf(
				"%w: only the member relation applies to a group",
				ErrInvalidTuple,
			)
		}
		return fgastore.GroupObjectKey(o.Group), fgastore.RelationGroupMember, nil
	default:
		return "", "", fmt.Errorf("%w: unknown object type", ErrInvalidTuple)
	}
}

// governingSpace returns the space that owns the records for an object: the space
// itself for a space object, or the group's space for a group object.
func governingSpace(object Object) (habitat_syntax.SpaceURI, error) {
	switch o := object.(type) {
	case SpaceObject:
		return o.Space, nil
	case GroupObject:
		_, space, _, _, _, err := habitat_syntax.ParseSpaceRecordURI(o.Group.String())
		if err != nil {
			return "", fmt.Errorf("%w: group object: %v", ErrInvalidTuple, err)
		}
		return space, nil
	default:
		return "", fmt.Errorf("%w: unknown object type", ErrInvalidTuple)
	}
}

// ownerContextualTuple grants the space owner the owner relation so owners always
// resolve through Check/ListUsers/ListObjects without a stored tuple.
func ownerContextualTuple(space habitat_syntax.SpaceURI) fgastore.Tuple {
	return fgastore.Tuple{
		User:     fgastore.MemberUserString(space.SpaceOwner()),
		Relation: fgastore.RelationSpaceOwner,
		Object:   fgastore.SpaceObjectKey(space),
	}
}
