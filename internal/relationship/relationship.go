// Package relationship implements app-managed ReBAC over spaces. It mirrors
// relationship tuples (subject, relation, object) into both AT Protocol records
// in the governing space (so they are org-owned and readable by other apps) and
// the OpenFGA graph (for Check/expand).
//
// Groups, and an org's member/admin sets, are modeled as spaces: there is no
// dedicated group or organization concept here. A group is a space (type
// network.habitat.group) and is referenced as a grantee via a SpaceRoleSubject
// over that group-space (role reader = "all members of the group").
package relationship

import (
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/fgastore"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Role is an app-facing access-control role granted on a space. The implied
// hierarchy (owner ⇒ manager ⇒ writer ⇒ reader) is enforced by the OpenFGA
// model, not here.
type Role string

const (
	RoleOwner   Role = "owner"
	RoleManager Role = "manager"
	RoleWriter  Role = "writer"
	RoleReader  Role = "reader"
)

var (
	// ErrInvalidTuple is returned when a (subject, relation, object) combination
	// is not valid — an unknown role, or a subject that cannot be parsed.
	ErrInvalidTuple = errors.New("invalid relationship tuple")
	// ErrTupleNotFound is returned when no tuple record exists at a given URI.
	ErrTupleNotFound = errors.New("relationship tuple not found")
)

// lexicon $type discriminators for the subject union.
const (
	subjectTypeUser      = "network.habitat.relationship.defs#userSubject"
	subjectTypeSpaceRole = "network.habitat.relationship.defs#spaceRoleSubject"
)

// SubjectKind distinguishes the concrete kinds in the subject union, matching
// the listTuples subjectType filter values.
type SubjectKind string

const (
	SubjectKindUser  SubjectKind = "user"
	SubjectKindSpace SubjectKind = "space"
)

// roleToFGARelation maps an app role to its OpenFGA relation name. This and its
// inverse are the single point where app role names cross into OpenFGA's
// vocabulary.
func roleToFGARelation(role Role) (string, error) {
	switch role {
	case RoleOwner:
		return fgastore.RelationSpaceOwner, nil
	case RoleManager:
		return fgastore.RelationSpaceMemberManager, nil
	case RoleWriter:
		return fgastore.RelationSpaceWriter, nil
	case RoleReader:
		return fgastore.RelationSpaceReader, nil
	default:
		return "", fmt.Errorf("%w: unknown role %q", ErrInvalidTuple, role)
	}
}

// Subject is a grantee in a relationship tuple: either an individual user
// (UserSubject) or a space userset (SpaceRoleSubject) — all subjects holding a
// role on a space, used for groups (which are spaces) and cross-space
// inheritance.
type Subject interface {
	isSubject()
	// Kind reports the concrete subject kind.
	Kind() SubjectKind
	// fgaUserString returns the OpenFGA user string for the subject.
	fgaUserString() (string, error)
	// toInterface serializes the subject to the lexicon union map form stored in
	// the AT Protocol record and returned over XRPC.
	toInterface() map[string]any
}

// UserSubject is an individual user identified by DID.
type UserSubject struct {
	DID syntax.DID
}

func (UserSubject) isSubject()        {}
func (UserSubject) Kind() SubjectKind { return SubjectKindUser }

func (s UserSubject) fgaUserString() (string, error) {
	return fgastore.MemberUserString(s.DID), nil
}

func (s UserSubject) toInterface() map[string]any {
	return map[string]any{
		"$type": subjectTypeUser,
		"did":   s.DID.String(),
	}
}

// SpaceRoleSubject is all subjects holding Role on Space (a userset).
type SpaceRoleSubject struct {
	Space habitat_syntax.SpaceURI
	Role  Role
}

func (SpaceRoleSubject) isSubject()        {}
func (SpaceRoleSubject) Kind() SubjectKind { return SubjectKindSpace }

func (s SpaceRoleSubject) fgaUserString() (string, error) {
	relation, err := roleToFGARelation(s.Role)
	if err != nil {
		return "", err
	}
	return fgastore.SpaceUsersetString(s.Space, relation), nil
}

func (s SpaceRoleSubject) toInterface() map[string]any {
	return map[string]any{
		"$type": subjectTypeSpaceRole,
		"space": s.Space.String(),
		"role":  string(s.Role),
	}
}

// parseSubjectInput decodes a subject from the generated union interface{} value
// (as produced by JSON decoding of an XRPC body or CBOR decoding of a record).
func parseSubjectInput(generic any) (Subject, error) {
	m, ok := generic.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: subject is not an object", ErrInvalidTuple)
	}
	typ, _ := m["$type"].(string)
	switch typ {
	case subjectTypeUser:
		raw, ok := m["did"].(string)
		if !ok {
			return nil, fmt.Errorf("%w: user subject missing did", ErrInvalidTuple)
		}
		did, err := syntax.ParseDID(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid did: %v", ErrInvalidTuple, err)
		}
		return UserSubject{DID: did}, nil
	case subjectTypeSpaceRole:
		rawSpace, ok := m["space"].(string)
		if !ok {
			return nil, fmt.Errorf("%w: space-role subject missing space", ErrInvalidTuple)
		}
		space, err := habitat_syntax.ParseSpaceURI(rawSpace)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid space uri: %v", ErrInvalidTuple, err)
		}
		rawRole, ok := m["role"].(string)
		if !ok {
			return nil, fmt.Errorf("%w: space-role subject missing role", ErrInvalidTuple)
		}
		role := Role(rawRole)
		if _, err := roleToFGARelation(role); err != nil {
			return nil, err
		}
		return SpaceRoleSubject{Space: space, Role: role}, nil
	default:
		return nil, fmt.Errorf("%w: unknown subject $type %q", ErrInvalidTuple, typ)
	}
}

// parseSubjectParams builds a Subject from the flat check query params: a bare
// DID yields a UserSubject, while a space URI plus subjectRole yields a
// SpaceRoleSubject userset (all subjects holding subjectRole on that space).
// subjectRole is required for, and only valid with, a space subject.
func parseSubjectParams(subject, subjectRole string) (Subject, error) {
	if did, err := syntax.ParseDID(subject); err == nil {
		if subjectRole != "" {
			return nil, fmt.Errorf(
				"%w: subjectRole must be omitted for a user subject",
				ErrInvalidTuple,
			)
		}
		return UserSubject{DID: did}, nil
	}
	space, err := habitat_syntax.ParseSpaceURI(subject)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: subject is neither a DID nor a space URI",
			ErrInvalidTuple,
		)
	}
	if subjectRole == "" {
		return nil, fmt.Errorf(
			"%w: subjectRole is required for a space subject",
			ErrInvalidTuple,
		)
	}
	role := Role(subjectRole)
	if _, err := roleToFGARelation(role); err != nil {
		return nil, err
	}
	return SpaceRoleSubject{Space: space, Role: role}, nil
}

// objectToInterface serializes a space object (a plain ref, not a union, so no
// $type) to the map form stored in the record and returned over XRPC.
func objectToInterface(space habitat_syntax.SpaceURI) map[string]any {
	return map[string]any{"space": space.String()}
}

// parseObject decodes a space object from the generated object value.
func parseObject(generic any) (habitat_syntax.SpaceURI, error) {
	m, ok := generic.(map[string]any)
	if !ok {
		return "", fmt.Errorf("%w: object is not an object", ErrInvalidTuple)
	}
	raw, ok := m["space"].(string)
	if !ok {
		return "", fmt.Errorf("%w: object missing space", ErrInvalidTuple)
	}
	space, err := habitat_syntax.ParseSpaceURI(raw)
	if err != nil {
		return "", fmt.Errorf("%w: invalid object space uri: %v", ErrInvalidTuple, err)
	}
	return space, nil
}
