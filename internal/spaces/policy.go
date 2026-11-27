package spaces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// The baseline space policies every host of the permissioned-data proposal
// (0016) must support, identified by the $type of the policy object.
const (
	PolicyPersonalSpace = "com.atproto.personalspace.policy"
	PolicyMemberSpace   = "com.atproto.memberspace.policy"
	PolicyManagedSpace  = "com.atproto.managedspace.policy"
)

// PolicyGroupSpace is the Atmosphere Groups space policy (org.atgrp.policy):
// each role's access and permissions in the space, whether it is public, and
// whether it is left out of the group's space index. Only group hosts honour
// it, and only for spaces owned by a group.
const PolicyGroupSpace = "org.atgrp.policy"

// PolicyRelationSpace is habitat's own policy: access is decided by the
// space's userRelation and spaceRelation records (the perms store) rather than
// a memberspace member list. It carries no configuration.
const PolicyRelationSpace = "network.habitat.relationspace.policy"

var (
	// ErrUnsupportedPolicy is returned for a policy this host doesn't
	// implement (including managedspace, which needs a managing app).
	ErrUnsupportedPolicy = errors.New("unsupported space policy")
	// ErrInvalidPolicy is returned for a malformed policy object.
	ErrInvalidPolicy = errors.New("invalid space policy")
)

// Policy is a space's policy: the full configuration of how access to the
// space is decided. Type selects the policy; the remaining fields are the
// memberspace configuration and are zero for other policies.
type Policy struct {
	Type           string   `json:"$type"`
	PublicRead     bool     `json:"publicRead,omitempty"`
	PublicWrite    bool     `json:"publicWrite,omitempty"`
	AllowedApps    []string `json:"allowedApps,omitempty"`
	PublishPolicy  bool     `json:"publishPolicy,omitempty"`
	PublishMembers bool     `json:"publishMembers,omitempty"`

	// Unlisted and Grants configure the group policy and are zero otherwise.
	Unlisted bool         `json:"unlisted,omitempty"`
	Grants   []GroupGrant `json:"grants,omitempty"`
}

// GroupGrant is a role's grants in one space under the group policy. Read and
// Write apply to members acting as themselves, Permissions to acting as the
// group in the space.
type GroupGrant struct {
	Role        string   `json:"role"`
	Read        bool     `json:"read,omitempty"`
	Write       bool     `json:"write,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
}

// ReadRoles returns the roles the group policy lets read the space.
func (p Policy) ReadRoles() []string {
	var roles []string
	for _, grant := range p.Grants {
		if grant.Read {
			roles = append(roles, grant.Role)
		}
	}
	return roles
}

// WriteRoles returns the roles the group policy lets write to the space.
func (p Policy) WriteRoles() []string {
	var roles []string
	for _, grant := range p.Grants {
		if grant.Write {
			roles = append(roles, grant.Role)
		}
	}
	return roles
}

// DefaultPolicy is the policy of a space with none stored: a memberspace with
// no public access.
func DefaultPolicy() Policy { return Policy{Type: PolicyMemberSpace} }

// ParsePolicy decodes and validates a policy object. An empty/nil raw value
// yields [DefaultPolicy].
func ParsePolicy(raw json.RawMessage) (Policy, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return DefaultPolicy(), nil
	}
	var p Policy
	if err := json.Unmarshal(raw, &p); err != nil {
		return Policy{}, fmt.Errorf("%w: %w", ErrInvalidPolicy, err)
	}
	switch p.Type {
	case PolicyPersonalSpace:
		// No configuration: drop anything extra the caller sent.
		return Policy{Type: PolicyPersonalSpace}, nil
	case PolicyRelationSpace:
		return Policy{Type: PolicyRelationSpace}, nil
	case PolicyMemberSpace:
		return p, nil
	case PolicyGroupSpace:
		seen := map[string]bool{}
		for _, grant := range p.Grants {
			if grant.Role == "" || seen[grant.Role] {
				return Policy{}, fmt.Errorf("%w: grants need one entry per role", ErrInvalidPolicy)
			}
			seen[grant.Role] = true
		}
		return Policy{
			Type: PolicyGroupSpace, PublicRead: p.PublicRead, Unlisted: p.Unlisted,
			Grants: p.Grants,
		}, nil
	case PolicyManagedSpace:
		return Policy{}, fmt.Errorf("%w: %s", ErrUnsupportedPolicy, p.Type)
	case "":
		return Policy{}, fmt.Errorf("%w: missing $type", ErrInvalidPolicy)
	default:
		return Policy{}, fmt.Errorf("%w: %s", ErrUnsupportedPolicy, p.Type)
	}
}

// GetSpacePolicy returns the space's policy, or [ErrSpaceNotFound].
func (s *store) GetSpacePolicy(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
) (Policy, error) {
	var row space
	err := s.db.WithContext(ctx).
		Where("owner = ? AND type = ? AND skey = ?", uri.SpaceOwner(), uri.SpaceType(), uri.Skey()).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Policy{}, ErrSpaceNotFound
	} else if err != nil {
		return Policy{}, err
	}
	return ParsePolicy(row.Policy)
}

// SetSpacePolicy replaces the space's policy wholesale, or returns
// [ErrSpaceNotFound].
func (s *store) SetSpacePolicy(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	policy Policy,
) error {
	raw, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	res := s.db.WithContext(ctx).Model(&space{}).
		Where("owner = ? AND type = ? AND skey = ?", uri.SpaceOwner(), uri.SpaceType(), uri.Skey()).
		Update("policy", raw)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrSpaceNotFound
	}
	return nil
}
