package relationship

import (
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// asObjectMap coerces a decoded JSON/CBOR value into a string-keyed map.
func asObjectMap(raw any) (map[string]any, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: expected an object, got %T", ErrInvalidTuple, raw)
	}
	return m, nil
}

func stringField(m map[string]any, key string) (string, error) {
	v, ok := m[key]
	if !ok {
		return "", fmt.Errorf("%w: missing field %q", ErrInvalidTuple, key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%w: field %q is not a string", ErrInvalidTuple, key)
	}
	return s, nil
}

func parseGroupURI(raw string) (habitat_syntax.SpaceRecordURI, error) {
	uri, _, _, _, _, err := habitat_syntax.ParseSpaceRecordURI(raw)
	if err != nil {
		return "", fmt.Errorf("%w: invalid group uri: %v", ErrInvalidTuple, err)
	}
	return uri, nil
}

// ParseSubject decodes a relationship subject union value.
func ParseSubject(raw any) (Subject, error) {
	m, err := asObjectMap(raw)
	if err != nil {
		return nil, err
	}
	typ, err := stringField(m, "$type")
	if err != nil {
		return nil, err
	}
	switch typ {
	case typeUserSubject:
		didStr, err := stringField(m, "did")
		if err != nil {
			return nil, err
		}
		did, err := syntax.ParseDID(didStr)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidTuple, err)
		}
		return UserSubject{DID: did}, nil
	case typeGroupSubject:
		groupStr, err := stringField(m, "group")
		if err != nil {
			return nil, err
		}
		group, err := parseGroupURI(groupStr)
		if err != nil {
			return nil, err
		}
		return GroupSubject{Group: group}, nil
	case typeSpaceRoleSubject:
		spaceStr, err := stringField(m, "space")
		if err != nil {
			return nil, err
		}
		space, err := habitat_syntax.ParseSpaceURI(spaceStr)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidTuple, err)
		}
		roleStr, err := stringField(m, "role")
		if err != nil {
			return nil, err
		}
		return SpaceRoleSubject{Space: space, Role: Role(roleStr)}, nil
	case typeOrgRoleSubject:
		orgStr, err := stringField(m, "org")
		if err != nil {
			return nil, err
		}
		org, err := syntax.ParseDID(orgStr)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidTuple, err)
		}
		roleStr, err := stringField(m, "role")
		if err != nil {
			return nil, err
		}
		return OrgRoleSubject{Org: org, Role: Role(roleStr)}, nil
	default:
		return nil, fmt.Errorf("%w: unknown subject $type %q", ErrInvalidTuple, typ)
	}
}

// ParseObject decodes a relationship object union value.
func ParseObject(raw any) (Object, error) {
	m, err := asObjectMap(raw)
	if err != nil {
		return nil, err
	}
	typ, err := stringField(m, "$type")
	if err != nil {
		return nil, err
	}
	switch typ {
	case typeSpaceObject:
		spaceStr, err := stringField(m, "space")
		if err != nil {
			return nil, err
		}
		space, err := habitat_syntax.ParseSpaceURI(spaceStr)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidTuple, err)
		}
		return SpaceObject{Space: space}, nil
	case typeGroupObject:
		groupStr, err := stringField(m, "group")
		if err != nil {
			return nil, err
		}
		group, err := parseGroupURI(groupStr)
		if err != nil {
			return nil, err
		}
		return GroupObject{Group: group}, nil
	default:
		return nil, fmt.Errorf("%w: unknown object $type %q", ErrInvalidTuple, typ)
	}
}

// subjectValue serializes a subject to a union map for record storage / output.
func subjectValue(s Subject) map[string]any {
	switch s := s.(type) {
	case UserSubject:
		return map[string]any{"$type": typeUserSubject, "did": s.DID.String()}
	case GroupSubject:
		return map[string]any{"$type": typeGroupSubject, "group": s.Group.String()}
	case SpaceRoleSubject:
		return map[string]any{
			"$type": typeSpaceRoleSubject,
			"space": s.Space.String(),
			"role":  string(s.Role),
		}
	case OrgRoleSubject:
		return map[string]any{
			"$type": typeOrgRoleSubject,
			"org":   s.Org.String(),
			"role":  string(s.Role),
		}
	default:
		return nil
	}
}

// objectValue serializes an object to a union map for record storage / output.
func objectValue(o Object) map[string]any {
	switch o := o.(type) {
	case SpaceObject:
		return map[string]any{"$type": typeSpaceObject, "space": o.Space.String()}
	case GroupObject:
		return map[string]any{"$type": typeGroupObject, "group": o.Group.String()}
	default:
		return nil
	}
}
