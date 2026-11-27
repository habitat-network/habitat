package spaces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/habitat-network/habitat/api/comatproto"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// The baseline space policies every host of the permissioned-data proposal
// (0016) must support, identified by the $type of the policy object.
const (
	PolicyPersonalSpace = "com.atproto.personalspace.policy"
	PolicyMemberSpace   = "com.atproto.memberspace.policy"
	PolicyManagedSpace  = "com.atproto.managedspace.policy"
)

// PolicyRelationSpace is habitat's own policy: access is decided by the
// space's userRelation and spaceRelation records (the perms store) rather than
// a memberspace member list. It carries no configuration.
const PolicyRelationSpace = "network.habitat.relationspace.policy"

// PolicyAtgrp is the Atmosphere Groups policy. It does not inherit grants
// from the baseline policies or from Habitat space relationships.
const PolicyAtgrp = "org.atgrp.policy"

var (
	// ErrUnsupportedPolicy is returned for a policy this host doesn't
	// implement (including managedspace, which needs a managing app).
	ErrUnsupportedPolicy = errors.New("unsupported space policy")
	// ErrInvalidPolicy is returned for a malformed policy object.
	ErrInvalidPolicy = errors.New("invalid space policy")
	// ErrReservedSpace is returned if a generic lifecycle operation targets a
	// reserved group configuration space.
	ErrReservedSpace = errors.New("group configuration space is reserved")
)

// Policy is a space's policy: the full configuration of how access to the
// space is decided. Type selects which configuration fields apply.
type Policy struct {
	Type           string       `json:"$type"`
	PublicRead     bool         `json:"publicRead,omitempty"`
	PublicWrite    bool         `json:"publicWrite,omitempty"`
	AllowedApps    []string     `json:"allowedApps,omitempty"`
	PublishPolicy  bool         `json:"publishPolicy,omitempty"`
	PublishMembers bool         `json:"publishMembers,omitempty"`
	Unlisted       bool         `json:"unlisted,omitempty"`
	Grants         []AtgrpGrant `json:"-"`
}

// AtgrpGrant describes one role's personal access and group-account
// permissions in a single space.
type AtgrpGrant struct {
	Role        string   `json:"role"`
	Read        bool     `json:"read,omitempty"`
	Write       bool     `json:"write,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
}

type atgrpPolicyJSON struct {
	Type       string       `json:"$type"`
	PublicRead bool         `json:"publicRead,omitempty"`
	Unlisted   bool         `json:"unlisted,omitempty"`
	Grants     []AtgrpGrant `json:"grants"`
}

func (p Policy) MarshalJSON() ([]byte, error) {
	if p.Type == PolicyAtgrp {
		grants := p.Grants
		if grants == nil {
			grants = []AtgrpGrant{}
		}
		return json.Marshal(atgrpPolicyJSON{
			Type: p.Type, PublicRead: p.PublicRead, Unlisted: p.Unlisted, Grants: grants,
		})
	}
	type alias Policy
	return json.Marshal(alias(p))
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
		p.Unlisted = false
		return p, nil
	case PolicyAtgrp:
		var group atgrpPolicyJSON
		if err := json.Unmarshal(raw, &group); err != nil {
			return Policy{}, fmt.Errorf("%w: %w", ErrInvalidPolicy, err)
		}
		if group.Grants == nil || len(group.Grants) > 100 {
			return Policy{}, fmt.Errorf("%w: grants must be an array with at most 100 entries", ErrInvalidPolicy)
		}
		seen := make(map[string]bool, len(group.Grants))
		for _, grant := range group.Grants {
			if _, err := syntax.ParseRecordKey(grant.Role); err != nil {
				return Policy{}, fmt.Errorf("%w: invalid grant role: %w", ErrInvalidPolicy, err)
			}
			if seen[grant.Role] {
				return Policy{}, fmt.Errorf("%w: duplicate grant for role %q", ErrInvalidPolicy, grant.Role)
			}
			seen[grant.Role] = true
			if len(grant.Permissions) > 100 {
				return Policy{}, fmt.Errorf("%w: grant has more than 100 permissions", ErrInvalidPolicy)
			}
			for _, permission := range grant.Permissions {
				if len(permission) == 0 || len(permission) > 2000 {
					return Policy{}, fmt.Errorf("%w: invalid grant permission length", ErrInvalidPolicy)
				}
			}
		}
		return Policy{
			Type: group.Type, PublicRead: group.PublicRead, Unlisted: group.Unlisted, Grants: group.Grants,
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
	return s.getSpacePolicy(ctx, uri, false)
}

func (s *store) getSpacePolicy(ctx context.Context, uri habitat_syntax.SpaceURI, lock bool) (Policy, error) {
	var row space
	query := s.spacePolicyRow(ctx, uri)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := query.First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Policy{}, ErrSpaceNotFound
	} else if err != nil {
		return Policy{}, err
	}
	return ParsePolicy(row.Policy)
}

// SetSpacePolicy replaces a non-group space's policy.
func (s *store) SetSpacePolicy(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	policy Policy,
) error {
	if policy.Type == PolicyAtgrp || habitat_syntax.IsAtgrpConfigurationSpaceType(uri.SpaceType()) {
		return fmt.Errorf("%w: group policy requires group space management", ErrUnsupportedPolicy)
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		group := s.withTx(tx)
		current, err := group.getSpacePolicy(ctx, uri, true)
		if err != nil {
			return err
		}
		if current.Type == PolicyAtgrp {
			return fmt.Errorf("%w: group policy requires group space management", ErrUnsupportedPolicy)
		}
		return group.saveSpacePolicy(ctx, uri, policy)
	})
}

// SetGroupSpacePolicy stores and publishes the canonical policy in the same
// transaction. This is a trusted primitive for provisioning and group space
// management. The caller must already have authorized the change, and can
// pass a transaction-bound store to include it in a larger operation.
func SetGroupSpacePolicy(ctx context.Context, store Store, uri habitat_syntax.SpaceURI, policy Policy) error {
	return store.setGroupSpacePolicy(ctx, uri, policy)
}

func (s *store) setGroupSpacePolicy(ctx context.Context, uri habitat_syntax.SpaceURI, policy Policy) error {
	if policy.Type != PolicyAtgrp {
		return fmt.Errorf("%w: expected a group policy", ErrUnsupportedPolicy)
	}
	if habitat_syntax.IsAtgrpConfigurationSpaceType(uri.SpaceType()) {
		if uri.Skey() != "self" {
			return ErrReservedSpace
		}
		policy.Unlisted = false
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	policy, err = ParsePolicy(raw)
	if err != nil {
		return err
	}
	value, err := MarshalRecord(comatproto.ComAtprotoSpacePolicy{Policy: policy})
	if err != nil {
		return fmt.Errorf("marshal group policy record: %w", err)
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		group := s.withTx(tx)
		// Lock the authority's repo before the space row, in the same order
		// as PutRecord, to keep the record and private policy in step.
		if err := lockRepo(tx, uri, uri.SpaceOwner()); err != nil {
			return err
		}
		if err := group.saveSpacePolicy(ctx, uri, policy); err != nil {
			return err
		}
		_, _, err := group.PutRecord(ctx, uri, uri.SpaceOwner(), habitat_syntax.SpacePolicyCollection, "self", value)
		return err
	})
}

func (s *store) saveSpacePolicy(ctx context.Context, uri habitat_syntax.SpaceURI, policy Policy) error {
	raw, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	res := s.spacePolicyRow(ctx, uri).Update("policy", raw)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrSpaceNotFound
	}
	return nil
}

func (s *store) spacePolicyRow(ctx context.Context, uri habitat_syntax.SpaceURI) *gorm.DB {
	return s.db.WithContext(ctx).
		Where("owner = ? AND type = ? AND skey = ?", uri.SpaceOwner(), uri.SpaceType(), uri.Skey()).
		Model(&space{})
}
