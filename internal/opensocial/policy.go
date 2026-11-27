package opensocial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"go.opentelemetry.io/otel/trace"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/telemetry"
)

// GetPolicy returns space's group policy, or spaces.ErrSpaceNotFound. A space
// that uses another policy type has no group grants, so it yields a group
// policy with none.
func (s *Store) GetPolicy(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) (spaces.Policy, error) {
	policy, err := s.spacesStore.GetSpacePolicy(ctx, space)
	if err != nil {
		return spaces.Policy{}, err
	}
	if policy.Type != spaces.PolicyGroupSpace {
		return spaces.Policy{Type: spaces.PolicyGroupSpace}, nil
	}
	return policy, nil
}

// publishedPolicy is the com.atproto.space.policy record for policy.
func publishedPolicy(policy spaces.Policy) (map[string]any, error) {
	raw, err := json.Marshal(policy)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return map[string]any{"policy": value}, nil
}

// SetPolicy stores policy on space, publishes it in the space as a
// com.atproto.space.policy record, and keeps the group's space index in step.
// It performs no authorization check; callers enforce the escalation rule.
func (s *Store) SetPolicy(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	policy spaces.Policy,
) error {
	policy.Type = spaces.PolicyGroupSpace
	if err := s.spacesStore.SetSpacePolicy(ctx, space, policy); err != nil {
		return fmt.Errorf("set space policy: %w", err)
	}
	value, err := publishedPolicy(policy)
	if err != nil {
		return fmt.Errorf("encode published policy: %w", err)
	}
	recordBytes, err := spaces.MarshalRecord(value)
	if err != nil {
		return fmt.Errorf("marshal published policy: %w", err)
	}
	if _, _, err := s.spacesStore.PutRecord(
		ctx, space, space.SpaceOwner(), SpacePolicyCollection, "self", recordBytes,
	); err != nil {
		return fmt.Errorf("put published policy: %w", err)
	}
	return s.syncIndex(ctx, space, policy)
}

// isConfigSpace reports whether space is the group's meta or members space,
// which are never indexed.
func isConfigSpace(space habitat_syntax.SpaceURI) bool {
	t := space.SpaceType()
	return t == MetaSpaceType || t == MembersSpaceType
}

// syncIndex makes the group's space index list space exactly when its policy
// does not leave it unlisted. The index is a discovery aid, so it matches
// entries by space rather than tracking their keys.
func (s *Store) syncIndex(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	policy spaces.Policy,
) error {
	if isConfigSpace(space) {
		return nil
	}
	orgDID := space.SpaceOwner()
	membersSpace := membersSpaceOf(orgDID)
	collection := syntax.NSID(SpaceIndexCollection)
	entries, err := s.spacesStore.ListRecords(ctx, membersSpace, orgDID, &collection)
	if err != nil {
		return fmt.Errorf("list space index: %w", err)
	}
	var existing []spaces.Record
	for _, entry := range entries {
		indexed, _ := entry.Value["space"].(string)
		// Index entries use either URI form; compare them canonically.
		if habitat_syntax.SpaceURI(indexed).Canonical() == space.Canonical() {
			existing = append(existing, entry)
		}
	}
	if policy.Unlisted {
		for _, entry := range existing {
			if err := s.spacesStore.DeleteRecord(
				ctx, membersSpace, orgDID, collection, string(entry.Rkey),
			); err != nil {
				return fmt.Errorf("delete space index entry: %w", err)
			}
		}
		return nil
	}
	if len(existing) > 0 {
		return nil
	}
	// New records name the space in the at:space/ form clients now write; older
	// ones keep the at:// form until the URI migration.
	recordBytes, err := spaces.MarshalRecord(opensocial_api.OrgAtgrpSpace{
		Space:     space.AtSpace().String(),
		CreatedAt: time.Now().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("marshal space record: %w", err)
	}
	if _, _, err := s.spacesStore.PutRecord(
		ctx, membersSpace, orgDID, SpaceIndexCollection, "", recordBytes,
	); err != nil {
		return fmt.Errorf("put space record: %w", err)
	}
	return nil
}

// dropIndex removes space's entries from the group's space index.
func (s *Store) dropIndex(ctx context.Context, space habitat_syntax.SpaceURI) error {
	return s.syncIndex(ctx, space, spaces.Policy{Unlisted: true})
}

// HoldsRead reports whether roles may read a space under policy.
func HoldsRead(policy spaces.Policy, roles []string) bool {
	return policy.PublicRead || slices.ContainsFunc(roles, func(r string) bool {
		return slices.Contains(policy.ReadRoles(), r)
	})
}

// HoldsWrite reports whether roles may write to a space under policy.
func HoldsWrite(policy spaces.Policy, roles []string) bool {
	return slices.ContainsFunc(roles, func(r string) bool {
		return slices.Contains(policy.WriteRoles(), r)
	})
}

// CheckPermission reports whether user may read space: the group DID always
// may, anyone may when the policy is public, and members may when one of
// their roles has read access in the space's policy.
func (s *Store) CheckPermission(
	ctx context.Context,
	user syntax.DID,
	space habitat_syntax.SpaceURI,
) (ok bool, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.CheckPermission", trace.WithAttributes(
		memberAttr(user), telemetry.Space(space),
	))
	defer func() {
		span.SetAttributes(allowedKey.Bool(ok))
		telemetry.End(span, err)
	}()
	// The group DID is implicitly a member that can read every group space.
	if user == space.SpaceOwner() {
		return true, nil
	}
	policy, err := s.spacesStore.GetSpacePolicy(ctx, space)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get space policy: %w", err)
	}
	if policy.Type != spaces.PolicyGroupSpace {
		return false, nil
	}
	if policy.PublicRead {
		return true, nil
	}
	roles, err := s.GetUserRoles(ctx, space.SpaceOwner(), user)
	if err != nil {
		return false, fmt.Errorf("get user roles: %w", err)
	}
	return HoldsRead(policy, roles), nil
}

// CheckWrite reports whether user may write their own records to space: the
// group DID always may, and members may when one of their roles has write
// access in the space's policy. Making a space public never lets a
// non-member write.
func (s *Store) CheckWrite(
	ctx context.Context,
	user syntax.DID,
	space habitat_syntax.SpaceURI,
) (bool, error) {
	if user == space.SpaceOwner() {
		return true, nil
	}
	policy, err := s.spacesStore.GetSpacePolicy(ctx, space)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get space policy: %w", err)
	}
	if policy.Type != spaces.PolicyGroupSpace {
		return false, nil
	}
	roles, err := s.GetUserRoles(ctx, space.SpaceOwner(), user)
	if err != nil {
		return false, fmt.Errorf("get user roles: %w", err)
	}
	return HoldsWrite(policy, roles), nil
}
