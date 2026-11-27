package opensocial

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

var (
	// ErrReservedSpace is returned for the meta, members and inbox spaces,
	// which a group cannot create or delete through createSpace/deleteSpace.
	ErrReservedSpace = errors.New("the meta, members and inbox spaces are reserved")
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
)

func isReservedSpaceType(spaceType syntax.NSID) bool {
	switch spaceType.String() {
	case MetaSpaceType, MembersSpaceType, InboxSpaceType:
		return true
	}
	return false
}

// validPermission reports whether permission is a scope a policy or role may
// grant.
func validPermission(permission string) bool {
	for _, prefix := range []string{includePrefix, spacePrefix, rpcPrefix, "habitat:"} {
		if strings.HasPrefix(permission, prefix) && len(permission) > len(prefix) {
			return true
		}
	}
	return false
}

// CreateGroupSpace creates a modality space under the group with the given
// policy and, unless the policy is unlisted, an entry in the group's space
// index. It implements com.atproto.space.createSpace for a group authority:
// the caller needs `space:*?manage=create`, every role in the policy must be
// declared, and the permissions it grants must be valid and held by the caller.
func (s *Store) CreateGroupSpace(
	ctx context.Context,
	orgDID, actor syntax.DID,
	spaceType syntax.NSID,
	skey habitat_syntax.SpaceKey,
	policy spaces.Policy,
) (habitat_syntax.SpaceURI, error) {
	if isReservedSpaceType(spaceType) {
		return "", ErrReservedSpace
	}
	scopes, err := s.UserScopes(ctx, orgDID, actor)
	if err != nil {
		return "", err
	}
	if !scopes.Covers("space:*?manage=create") {
		return "", fmt.Errorf("%w: missing manage=create", ErrEscalation)
	}
	declared, err := s.listRoleRkeys(ctx, orgDID)
	if err != nil {
		return "", err
	}
	assign, err := s.AssignableRoles(ctx, orgDID, actor)
	if err != nil {
		return "", err
	}
	for _, grant := range policy.Grants {
		if !slices.Contains(declared, grant.Role) {
			return "", fmt.Errorf("%w: %q", ErrUnknownRole, grant.Role)
		}
		for _, permission := range grant.Permissions {
			if !validPermission(permission) {
				return "", fmt.Errorf("%w: %q", ErrInvalidScope, permission)
			}
		}
		if actor != orgDID {
			if err := checkGrants(
				scopes, assign, grant.Role, nil, grant.Permissions, nil, nil,
			); err != nil {
				return "", err
			}
		}
	}
	return s.createSpace(ctx, orgDID, spaceType, skey, policy)
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
	if err := s.dropIndex(ctx, spaceURI); err != nil {
		return err
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
