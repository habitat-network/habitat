package atgrp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"

	atgrp_api "github.com/habitat-network/habitat/api/atgrp"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

var (
	ErrWriteForbidden = errors.New("group record write is not authorized")
	ErrRoleInUse      = errors.New("role is still used by a membership or space policy")
)

// GroupWritePlan describes the checks the caller must make after applying
// writes. Both checks and writes belong to the same WithGroupTx, so a failed
// check rolls back the whole batch. Reconcile affected writers before commit.
type GroupWritePlan struct {
	ChangedMembers []syntax.DID
	ChangedRoles   bool
	deletedRoles   []string
	keepAdmin      bool
}

// AuthorizeGroupRecordWrites checks group-authored writes against the issued
// token ceiling and against one pre-write snapshot of the authorizer's live
// roles. The caller must authenticate the group and authorizer first.
func (s *Store) AuthorizeGroupRecordWrites(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	authorizer syntax.DID,
	scopes []string,
	writes []spaces.Write,
) (GroupWritePlan, error) {
	var plan GroupWritePlan
	group := space.SpaceOwner()
	if authorizer == "" || authorizer == group {
		return plan, fmt.Errorf("%w: missing group session authorizer", ErrWriteForbidden)
	}
	policy, err := s.spaces.GetSpacePolicy(ctx, space)
	if err != nil {
		return plan, err
	}
	if policy.Type != spaces.PolicyAtgrp {
		return plan, fmt.Errorf("%w: not a group policy", ErrWriteForbidden)
	}
	actor, err := s.LoadAccess(ctx, group, authorizer)
	if err != nil {
		return plan, err
	}
	if !actor.Member {
		return plan, fmt.Errorf("%w: authorizer is no longer a member", ErrWriteForbidden)
	}
	live, token := actor.SpacePermissions(space, policy), ParsePermissions(group, scopes)
	check := recordWriteCheck{
		store: s, group: group, space: space, policy: policy, actor: actor,
		createdRoles: make(map[string]bool),
	}
	seen := make(map[[2]string]bool)
	for _, write := range writes {
		if !live.AllowsRecord(space, write.Collection, string(write.Action)) ||
			!token.AllowsRecord(space, write.Collection, string(write.Action)) {
			return plan, fmt.Errorf(
				"%w: insufficient scope for %s",
				ErrWriteForbidden,
				write.Collection,
			)
		}
		if err := checkRecordLocation(space, write.Collection); err != nil {
			return plan, err
		}
		if write.Collection != RoleCollection && write.Collection != MembershipCollection {
			continue
		}
		path := [2]string{write.Collection.String(), write.Rkey.String()}
		if seen[path] {
			return plan, fmt.Errorf("%w: duplicate role or membership write", ErrInvalidRecord)
		}
		seen[path] = true
		if write.Collection == RoleCollection && write.Action == spaces.WriteCreate {
			check.createdRoles[write.Rkey.String()] = true
		}
	}
	for _, write := range writes {
		switch write.Collection {
		case RoleCollection:
			if err := check.role(ctx, write); err != nil {
				return plan, err
			}
			plan.ChangedRoles = true
			if write.Action == spaces.WriteDelete {
				plan.deletedRoles = append(plan.deletedRoles, write.Rkey.String())
			}
		case MembershipCollection:
			member, err := check.membership(ctx, write)
			if err != nil {
				return plan, err
			}
			plan.ChangedMembers = append(plan.ChangedMembers, member)
		case "org.atgrp.config":
			if write.Rkey != "self" || write.Action == spaces.WriteDelete {
				return plan, fmt.Errorf("%w: group config must stay at self", ErrWriteForbidden)
			}
			var config atgrp_api.OrgAtgrpConfig
			if err := decodeWrite(write, &config); err != nil {
				return plan, err
			}
			if config.JoinPolicy != "invite" {
				return plan, fmt.Errorf(
					"%w: this host supports invite-only joining",
					ErrWriteForbidden,
				)
			}
		case "org.atgrp.about":
			if write.Rkey != "self" {
				return plan, fmt.Errorf("%w: group about record must use self", ErrInvalidRecord)
			}
		}
	}
	if plan.ChangedRoles || len(plan.ChangedMembers) > 0 {
		plan.keepAdmin, err = s.hasRecoveryMember(ctx, group, "", policy)
	}
	return plan, err
}

// AuthorizePersonalRecordWrites checks a personal OAuth token against the
// member's live write access. In the members space, only the member's own
// confirmation is personal data.
func (s *Store) AuthorizePersonalRecordWrites(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	member syntax.DID,
	scopes []string,
	writes []spaces.Write,
) error {
	group := space.SpaceOwner()
	if member == "" || member == group {
		return fmt.Errorf("%w: personal member required", ErrWriteForbidden)
	}
	policy, err := s.spaces.GetSpacePolicy(ctx, space)
	if err != nil {
		return err
	}
	actor, err := s.LoadAccess(ctx, group, member)
	if err != nil {
		return err
	}
	_, writer := actor.PersonalSpaceAccess(space, policy)
	if !writer {
		return fmt.Errorf("%w: member does not have current write access", ErrWriteForbidden)
	}
	token := ParsePermissions(member, scopes)
	for _, write := range writes {
		if !token.AllowsRecord(space, write.Collection, string(write.Action)) {
			return fmt.Errorf("%w: missing personal write scope", ErrWriteForbidden)
		}
		switch {
		case space == MembersSpace(group) && write.Collection == ConfirmationCollection:
			if write.Rkey != "self" {
				return fmt.Errorf("%w: confirmation must use self", ErrInvalidRecord)
			}
			if write.Action != spaces.WriteDelete {
				var confirmation atgrp_api.OrgAtgrpConfirmation
				if err := decodeWrite(write, &confirmation); err != nil {
					return err
				}
				if _, err := syntax.ParseDatetime(confirmation.CreatedAt); err != nil {
					return fmt.Errorf(
						"%w: invalid confirmation createdAt: %w",
						ErrInvalidRecord,
						err,
					)
				}
			}
		case habitat_syntax.IsAtgrpConfigurationSpaceType(space.SpaceType()):
			return fmt.Errorf(
				"%w: only a confirmation can be written as the member",
				ErrWriteForbidden,
			)
		default:
			if err := checkRecordLocation(space, write.Collection); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateGroupWriteResult checks the proposed state inside the same group
// transaction. This permits a role deletion and removal of its assignments
// in one batch, without allowing an undefined role that could be revived.
func (s *Store) ValidateGroupWriteResult(
	ctx context.Context, group syntax.DID, plan GroupWritePlan,
) error {
	if len(plan.deletedRoles) > 0 {
		if err := s.checkDeletedRoles(ctx, group, plan.deletedRoles); err != nil {
			return err
		}
	}
	if plan.keepAdmin {
		ok, err := s.HasRecoveryMember(ctx, group)
		if err != nil {
			return err
		}
		if !ok {
			return ErrLastAdmin
		}
	}
	return nil
}

// HasRecoveryMember reports whether someone can administer the group, using
// current roles and the members-space policy. Policy updates use it before
// and after the change to avoid removing the last way to recover access.
func (s *Store) HasRecoveryMember(ctx context.Context, group syntax.DID) (bool, error) {
	policy, err := s.spaces.GetSpacePolicy(ctx, MembersSpace(group))
	if err != nil {
		return false, err
	}
	return s.hasRecoveryMember(ctx, group, "", policy)
}

func checkRecordLocation(space habitat_syntax.SpaceURI, collection syntax.NSID) error {
	switch collection {
	case RoleCollection, MembershipCollection, "org.atgrp.rule":
		if space == MembersSpace(space.SpaceOwner()) {
			return nil
		}
	case "org.atgrp.about", "org.atgrp.config":
		if space.SpaceType() == habitat_syntax.AtgrpMetaSpaceType && space.Skey() == "self" {
			return nil
		}
	case "com.atproto.label.record", habitat_syntax.SpacePolicyCollection:
		// Moderation is not enabled, and the host publishes space policy.
	default:
		if !strings.HasPrefix(collection.String(), "org.atgrp.") &&
			!habitat_syntax.IsAtgrpConfigurationSpaceType(space.SpaceType()) {
			return nil
		}
	}
	return fmt.Errorf(
		"%w: protected collection or incorrect space for %s",
		ErrWriteForbidden,
		collection,
	)
}

type recordWriteCheck struct {
	store        *Store
	group        syntax.DID
	space        habitat_syntax.SpaceURI
	policy       spaces.Policy
	actor        Access
	createdRoles map[string]bool
}

func (c recordWriteCheck) role(ctx context.Context, write spaces.Write) error {
	id := write.Rkey.String()
	if !ValidRoleID(id) {
		return fmt.Errorf("%w: invalid role ID %q", ErrInvalidRecord, id)
	}
	if !c.actor.CanAssignRole(id) {
		return fmt.Errorf("%w: authorizer cannot change role %q", ErrWriteForbidden, id)
	}
	var before atgrp_api.OrgAtgrpRole
	if write.Action != spaces.WriteCreate {
		var err error
		before, err = c.store.getRole(ctx, c.group, id)
		if err != nil {
			return err
		}
	}
	if write.Action == spaces.WriteDelete {
		return nil
	}

	var after atgrp_api.OrgAtgrpRole
	if err := decodeWrite(write, &after); err != nil {
		return err
	}
	if len(after.DisplayName) > 640 || len(after.Description) > 20000 ||
		len(after.CanAssign) > 100 || len(after.Permissions) > 100 {
		return fmt.Errorf("%w: role exceeds size limits", ErrInvalidRecord)
	}
	oldOwn := ParsePermissions(c.group, before.Permissions)
	held := c.actor.Permissions
	for _, scope := range after.Permissions {
		if len(scope) > 2000 {
			return fmt.Errorf("%w: role permission exceeds size limit", ErrInvalidRecord)
		}
		if slices.Contains(before.Permissions, scope) && (before.Default || !after.Default) {
			continue
		}
		requested, err := parseScope(c.group, scope)
		if err != nil || len(requested) == 0 {
			return fmt.Errorf("%w: unsupported role permission %q", ErrInvalidRecord, scope)
		}
		for _, want := range requested {
			// Making a role a default delegates it to every invitee, even
			// when its permission strings themselves were not edited.
			heldBefore := (before.Default || !after.Default) && oldOwn.allows(want)
			if heldBefore || held.allows(want) {
				continue
			}
			canDelegate, err := c.canDelegatePermission(ctx, want)
			if err != nil {
				return err
			}
			if !canDelegate {
				return fmt.Errorf(
					"%w: role adds a permission the authorizer lacks",
					ErrWriteForbidden,
				)
			}
		}
	}
	for _, role := range after.CanAssign {
		if role != wildcard && !ValidRoleID(role) {
			return fmt.Errorf("%w: invalid canAssign role %q", ErrInvalidRecord, role)
		}
		heldBefore := slices.Contains(before.CanAssign, wildcard) ||
			slices.Contains(before.CanAssign, role)
		if (after.Default && !before.Default || !heldBefore) && !c.actor.CanAssignRole(role) {
			return fmt.Errorf("%w: authorizer cannot delegate role %q", ErrWriteForbidden, role)
		}
	}
	if after.Default && !before.Default {
		return c.canDefaultPolicyGrants(ctx, id)
	}
	return nil
}

func (c recordWriteCheck) canDelegatePermission(
	ctx context.Context,
	want permission,
) (bool, error) {
	// A local policy can never cover a wildcard or other group authority.
	if want.resource != "space" || want.primary == wildcard || want.secondary != c.group.String() ||
		want.key == wildcard {
		return false, nil
	}
	space := habitat_syntax.ConstructSpaceURI(
		c.group,
		syntax.NSID(want.primary),
		habitat_syntax.SpaceKey(want.key),
	)
	if space == c.space {
		return c.actor.SpacePermissions(space, c.policy).allows(want), nil
	}
	policy, err := c.store.spaces.GetSpacePolicy(ctx, space)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return c.actor.SpacePermissions(space, policy).allows(want), nil
}

func (c recordWriteCheck) canDefaultPolicyGrants(ctx context.Context, id string) error {
	return c.store.visitGroupPolicies(
		ctx,
		c.group,
		func(space habitat_syntax.SpaceURI, policy spaces.Policy) error {
			for _, grant := range policy.Grants {
				if grant.Role != id {
					continue
				}
				read, write := c.actor.PersonalSpaceAccess(space, policy)
				if grant.Read && !read || grant.Write && !write {
					return fmt.Errorf(
						"%w: default role would grant personal space access the authorizer lacks",
						ErrWriteForbidden,
					)
				}
				held := c.actor.SpacePermissions(space, policy)
				roleOnly := Access{Member: true, group: c.group, Roles: []string{id}}
				for _, want := range roleOnly.spacePermissions(space, policy) {
					if !held.allows(want) {
						return fmt.Errorf(
							"%w: default role would grant a space permission the authorizer lacks",
							ErrWriteForbidden,
						)
					}
				}
			}
			return nil
		},
	)
}

func (c recordWriteCheck) membership(ctx context.Context, write spaces.Write) (syntax.DID, error) {
	if write.Action != spaces.WriteUpdate {
		return "", fmt.Errorf(
			"%w: membership changes require invite redemption or personal leave",
			ErrWriteForbidden,
		)
	}
	did := syntax.DID(write.Rkey)
	before, err := c.store.GetMembership(ctx, c.group, did)
	if err != nil {
		return "", err
	}
	var after atgrp_api.OrgAtgrpMembership
	if err := decodeWrite(write, &after); err != nil {
		return "", err
	}
	if err := validateMembership(after); err != nil {
		return "", err
	}
	if after.CreatedAt != before.CreatedAt {
		return "", fmt.Errorf("%w: membership createdAt cannot change", ErrInvalidRecord)
	}
	if !c.actor.CanChangeRoles(before.Roles, after.Roles) {
		return "", fmt.Errorf(
			"%w: authorizer cannot grant or revoke one or more roles",
			ErrWriteForbidden,
		)
	}
	for _, role := range after.Roles {
		if !c.createdRoles[role] {
			if _, err := c.store.getRole(ctx, c.group, role); err != nil {
				return "", fmt.Errorf("lookup assigned role %q: %w", role, err)
			}
		}
	}
	return did, nil
}

func decodeWrite(write spaces.Write, out any) error {
	value, err := atdata.UnmarshalCBOR(write.Value)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidRecord, err)
	}
	return decodeRecord(value, write.Collection.String(), out)
}

func (s *Store) checkDeletedRoles(ctx context.Context, group syntax.DID, deleted []string) error {
	space := MembersSpace(group)
	collection := syntax.NSID(MembershipCollection)
	records, err := s.spaces.ListRecords(ctx, space, group, &collection)
	if err != nil {
		return err
	}
	for _, record := range records {
		membership, err := readMembership(space, &record)
		if err != nil {
			return err
		}
		for _, role := range deleted {
			if slices.Contains(membership.Roles, role) {
				return fmt.Errorf("%w: %q", ErrRoleInUse, role)
			}
		}
	}
	return s.visitGroupPolicies(
		ctx,
		group,
		func(_ habitat_syntax.SpaceURI, policy spaces.Policy) error {
			for _, grant := range policy.Grants {
				if slices.Contains(deleted, grant.Role) {
					return fmt.Errorf("%w: %q", ErrRoleInUse, grant.Role)
				}
			}
			return nil
		},
	)
}

func (s *Store) visitGroupPolicies(
	ctx context.Context,
	group syntax.DID,
	f func(habitat_syntax.SpaceURI, spaces.Policy) error,
) error {
	uris, err := s.spaces.ListSpaces(ctx, group, &group, nil)
	if err != nil {
		return err
	}
	for _, space := range uris {
		policy, err := s.spaces.GetSpacePolicy(ctx, space)
		if errors.Is(err, spaces.ErrSpaceNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if policy.Type == spaces.PolicyAtgrp {
			if err := f(space, policy); err != nil {
				return err
			}
		}
	}
	return nil
}
