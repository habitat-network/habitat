package opensocial

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

var (
	// ErrReservedSpace is returned for the meta, members and invites spaces,
	// which a group cannot create or delete through createSpace/deleteSpace.
	ErrReservedSpace = errors.New("the meta, members and invites spaces are reserved")
	// ErrUnknownRole is returned when an access grant names a role the group
	// has not declared.
	ErrUnknownRole = errors.New("access names a role the group has not declared")
	// ErrInvalidScope is returned for a scope that is malformed.
	ErrInvalidScope = errors.New("invalid scope")
	// ErrGroupNotFound is returned when a group does not exist on this host.
	ErrGroupNotFound = errors.New("group not found")
	// ErrJoinClosed is returned when a group is invite-only and the caller has
	// no invitation.
	ErrJoinClosed = errors.New("the group is invite-only")
	// ErrJoinNeedsApproval is returned for groups that approve joins, which
	// this host does not yet queue.
	ErrJoinNeedsApproval = errors.New("join requests are not supported yet")
)

func isReservedSpaceType(spaceType syntax.NSID) bool {
	switch spaceType.String() {
	case MetaSpaceType, MembersSpaceType, InvitesSpaceType:
		return true
	}
	return false
}

// spaceActions are the bare action scopes an access record may grant a role
// in its own space.
var spaceActions = []string{"read", "read_self", "create", "update", "delete"}

func validScope(scope string) bool {
	if slices.Contains(spaceActions, scope) {
		return true
	}
	for _, prefix := range []string{includePrefix, spacePrefix, rpcPrefix, "habitat:"} {
		if strings.HasPrefix(scope, prefix) && len(scope) > len(prefix) {
			return true
		}
	}
	return false
}

// CreateGroupSpace creates a modality space under the group with the given
// access record and, when index is true, an entry in the group's space index.
// It implements org.atgrp.createSpace: the caller needs the manage=create
// scope, every role in the grants must be declared, and the scopes granted
// must be valid and held by the caller (a bare action scope may be granted by
// anyone who can create the space).
func (s *Store) CreateGroupSpace(
	ctx context.Context,
	orgDID, actor syntax.DID,
	spaceType syntax.NSID,
	skey habitat_syntax.SpaceKey,
	policy spaces.Policy,
	access opensocial_api.OrgAtgrpDefsAccessConfig,
	index bool,
) (createdSpace, error) {
	if isReservedSpaceType(spaceType) {
		return createdSpace{}, ErrReservedSpace
	}
	scopes, err := s.UserScopes(ctx, orgDID, actor)
	if err != nil {
		return createdSpace{}, err
	}
	if !scopes.Covers("space:*?manage=create") {
		return createdSpace{}, fmt.Errorf("%w: missing manage=create", ErrEscalation)
	}
	declared, err := s.listRoleRkeys(ctx, orgDID)
	if err != nil {
		return createdSpace{}, err
	}
	for _, grant := range access.Grants {
		if !slices.Contains(declared, grant.Role) {
			return createdSpace{}, fmt.Errorf("%w: %q", ErrUnknownRole, grant.Role)
		}
		for _, scope := range grant.Scopes {
			if !validScope(scope) {
				return createdSpace{}, fmt.Errorf("%w: %q", ErrInvalidScope, scope)
			}
			if !slices.Contains(spaceActions, scope) && !scopes.Covers(scope) {
				return createdSpace{}, fmt.Errorf("%w: scope %q", ErrEscalation, scope)
			}
		}
	}
	return s.createSpace(ctx, orgDID, spaceType, skey, policy,
		opensocial_api.OrgAtgrpAccess{
			Grants: access.Grants, PublicRead: access.PublicRead,
		}, index)
}

// DeleteGroupSpace deletes a modality space and its index entry. The meta and
// members spaces cannot be deleted.
func (s *Store) DeleteGroupSpace(
	ctx context.Context,
	orgDID, actor syntax.DID,
	spaceURI habitat_syntax.SpaceURI,
) error {
	if spaceURI.SpaceOwner() != orgDID {
		return spaces.ErrSpaceNotFound
	}
	if isReservedSpaceType(syntax.NSID(spaceURI.SpaceType())) {
		return ErrReservedSpace
	}
	scopes, err := s.UserScopes(ctx, orgDID, actor)
	if err != nil {
		return err
	}
	if !scopes.Covers("space:*?manage=delete") {
		return fmt.Errorf("%w: missing manage=delete", ErrEscalation)
	}
	if _, err := s.spacesStore.GetSpacePolicy(ctx, spaceURI); err != nil {
		return err
	}
	membersSpace := habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self")
	collection := syntax.NSID("org.atgrp.space")
	entries, err := s.spacesStore.ListRecords(ctx, membersSpace, orgDID, &collection)
	if err != nil {
		return fmt.Errorf("list space index: %w", err)
	}
	for _, entry := range entries {
		indexed, _ := entry.Value["space"].(string)
		// Index entries use either URI form; compare them canonically.
		if habitat_syntax.SpaceURI(indexed).Canonical() != spaceURI.Canonical() {
			continue
		}
		if err := s.spacesStore.DeleteRecord(
			ctx, membersSpace, orgDID, collection, string(entry.Rkey),
		); err != nil {
			return fmt.Errorf("delete space index entry: %w", err)
		}
	}
	if err := s.spacesStore.DeleteSpace(ctx, spaceURI); err != nil {
		return fmt.Errorf("delete space: %w", err)
	}
	return nil
}

// LeaveGroup deletes member's membership in orgDID. It is idempotent for a
// group the member already left.
func (s *Store) LeaveGroup(ctx context.Context, orgDID, member syntax.DID) error {
	membersSpace := habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self")
	if _, err := s.spacesStore.GetSpacePolicy(
		ctx,
		membersSpace,
	); errors.Is(
		err,
		spaces.ErrSpaceNotFound,
	) {
		return ErrGroupNotFound
	} else if err != nil {
		return err
	}
	err := s.spacesStore.DeleteRecord(
		ctx, membersSpace, orgDID, MembershipCollection, string(member),
	)
	if err != nil && !errors.Is(err, spaces.ErrRecordNotFound) {
		return fmt.Errorf("delete membership: %w", err)
	}
	return nil
}

// JoinPolicy returns orgDID's profile joinPolicy, or ErrGroupNotFound.
func (s *Store) JoinPolicy(ctx context.Context, orgDID syntax.DID) (string, error) {
	if _, err := s.spacesStore.GetSpacePolicy(
		ctx, habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self"),
	); errors.Is(err, spaces.ErrSpaceNotFound) {
		return "", ErrGroupNotFound
	} else if err != nil {
		return "", err
	}
	profile, err := s.GetProfile(ctx, orgDID)
	if err != nil {
		return "", err
	}
	return profile.JoinPolicy, nil
}

// DefaultRoles returns the roles marked default, which a member admitted
// without an invitation receives.
func (s *Store) DefaultRoles(ctx context.Context, orgDID syntax.DID) ([]string, error) {
	collection := syntax.NSID(RoleCollection)
	records, err := s.spacesStore.ListRecords(
		ctx, habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self"),
		orgDID, &collection,
	)
	if err != nil {
		return nil, fmt.Errorf("list role records: %w", err)
	}
	var roles []string
	for _, record := range records {
		if isDefault, _ := record.Value["default"].(bool); isDefault {
			roles = append(roles, string(record.Rkey))
		}
	}
	return roles, nil
}

// Admit writes member's membership with the group's default roles and returns
// a reference to it. It is the path for groups whose joinPolicy is open.
func (s *Store) Admit(ctx context.Context, orgDID, member syntax.DID) (StrongRef, []string, error) {
	if existing, err := s.GetUserRoles(ctx, orgDID, member); err != nil {
		return StrongRef{}, nil, err
	} else if len(existing) > 0 {
		return StrongRef{}, existing, nil
	}
	roles, err := s.DefaultRoles(ctx, orgDID)
	if err != nil {
		return StrongRef{}, nil, err
	}
	ref, err := putMembershipRef(ctx, s.spacesStore, orgDID, member, roles)
	if err != nil {
		return StrongRef{}, nil, err
	}
	return ref, roles, nil
}
