package atgrp

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Access is the current union of a member's declared roles. Membership does
// not require confirmation or a nonempty set of roles.
type Access struct {
	Member      bool
	Roles       []string
	Permissions PermissionSet
	group       syntax.DID
	assignable  map[string]bool
}

// LoadAccess reads current access from the group-authored membership and role
// records. Never use the implicit group account as the authorizer of a group
// OAuth token.
func (s *Store) LoadAccess(ctx context.Context, group, actor syntax.DID) (Access, error) {
	if group == "" || actor == "" {
		return Access{}, nil
	}
	if actor == group {
		return Access{
			Member: true, group: group, Permissions: PermissionSet{all: true},
			assignable: map[string]bool{wildcard: true},
		}, nil
	}
	membership, err := s.GetMembership(ctx, group, actor)
	if errors.Is(err, spaces.ErrRecordNotFound) {
		return Access{}, nil
	} else if err != nil {
		return Access{}, err
	}
	return s.loadRoles(ctx, group, membership.Roles)
}

func (s *Store) loadRoles(ctx context.Context, group syntax.DID, ids []string) (Access, error) {
	access := Access{Member: true, group: group, assignable: make(map[string]bool)}
	uris := make([]habitat_syntax.SpaceRecordURI, 0, len(ids))
	for _, id := range ids {
		uris = append(uris, habitat_syntax.ConstructSpaceRecordURI(
			MembersSpace(group), group, RoleCollection, syntax.RecordKey(id),
		))
	}
	if len(uris) == 0 {
		return access, nil
	}
	records, err := s.spaces.GetRecords(ctx, uris)
	if err != nil {
		return Access{}, fmt.Errorf("get roles: %w", err)
	}
	for _, record := range records {
		role, err := readRole(&record)
		if err != nil {
			return Access{}, err
		}
		access.Roles = append(access.Roles, record.Rkey.String())
		access.Permissions.grants = append(access.Permissions.grants,
			ParsePermissions(group, role.Permissions).grants...)
		for _, assignable := range role.CanAssign {
			if assignable == wildcard || ValidRoleID(assignable) {
				access.assignable[assignable] = true
			}
		}
	}
	slices.Sort(access.Roles)
	return access, nil
}

// CanAssignRole checks the role delegation union, including a literal "*"
// requested when editing another role's canAssign.
func (a Access) CanAssignRole(role string) bool {
	return a.Member && (a.assignable[wildcard] || a.assignable[role])
}

// CanChangeRoles requires authority over every role added or removed.
func (a Access) CanChangeRoles(current, next []string) bool {
	if !a.Member {
		return false
	}
	for _, role := range current {
		if !slices.Contains(next, role) && !a.CanAssignRole(role) {
			return false
		}
	}
	for _, role := range next {
		if !slices.Contains(current, role) && !a.CanAssignRole(role) {
			return false
		}
	}
	return true
}

func (a Access) canRecover(members habitat_syntax.SpaceURI, policy spaces.Policy) bool {
	if !a.CanAssignRole(wildcard) {
		return false
	}
	permissions := a.SpacePermissions(members, policy)
	return permissions.AllowsRecord(members, RoleCollection, "create") &&
		permissions.AllowsRecord(members, RoleCollection, "update") &&
		permissions.AllowsRecord(members, MembershipCollection, "update")
}

// PersonalSpaceAccess applies grants for a member acting as themselves. Public
// read does not grant write and role grants don't cross group boundaries.
func (a Access) PersonalSpaceAccess(
	space habitat_syntax.SpaceURI,
	policy spaces.Policy,
) (read, write bool) {
	if policy.Type != spaces.PolicyAtgrp {
		return false, false
	}
	read = policy.PublicRead
	if !a.Member || space.SpaceOwner() != a.group {
		return read, false
	}
	if a.Permissions.all {
		return true, true
	}
	for _, grant := range policy.Grants {
		if slices.Contains(a.Roles, grant.Role) {
			read = read || grant.Read
			write = write || grant.Write
		}
	}
	return read, write
}

// SpacePermissions combines group-wide permissions with the current per-space
// policy grants. The per-space contribution cannot apply outside this space,
// authorize creation of other spaces, or authorize RPCs.
func (a Access) SpacePermissions(
	space habitat_syntax.SpaceURI,
	policy spaces.Policy,
) PermissionSet {
	out := a.Permissions
	out.grants = slices.Clone(out.grants)
	out.grants = append(out.grants, a.spacePermissions(space, policy)...)
	return out
}

func (a Access) spacePermissions(space habitat_syntax.SpaceURI, policy spaces.Policy) []permission {
	if !a.Member || policy.Type != spaces.PolicyAtgrp || space.SpaceOwner() != a.group {
		return nil
	}
	var out []permission
	for _, grant := range policy.Grants {
		if !slices.Contains(a.Roles, grant.Role) {
			continue
		}
		parsed := ParsePermissions(a.group, grant.Permissions)
		for _, perm := range parsed.grants {
			if bound, ok := bindPolicyPermission(space, perm); ok {
				out = append(out, bound)
			}
		}
	}
	return out
}

func bindPolicyPermission(space habitat_syntax.SpaceURI, perm permission) (permission, bool) {
	if perm.resource != "space" || perm.operation == "manage:create" {
		return permission{}, false
	}
	bound := spacePermission(space, perm.operation)
	bound.collection = perm.collection
	return bound, perm.covers(bound)
}

// AuthorizeGroupScopes returns the intersection of the requested scopes and
// the authorizer's current role union. Use the returned scopes as the token's
// ceiling at issuance, then apply this same ceiling or concrete operation
// checks again against live roles at request time.
func (s *Store) AuthorizeGroupScopes(
	ctx context.Context, group, actor syntax.DID, requested []string,
) ([]string, error) {
	if actor == "" || actor == group {
		return nil, nil
	}
	access, err := s.LoadAccess(ctx, group, actor)
	if err != nil || !access.Member {
		return nil, err
	}
	held := access.Permissions
	wanted := ParsePermissions(group, requested)
	if slices.ContainsFunc(
		wanted.grants,
		func(p permission) bool { return p.resource == "space" },
	) {
		// Publishing the policy creates a group-authored record in every
		// canonical space, even when it is absent from the space index.
		uris, err := s.spaces.ListSpaces(ctx, group, &group, nil)
		if err != nil {
			return nil, err
		}
		for _, uri := range uris {
			policy, err := s.spaces.GetSpacePolicy(ctx, uri)
			if errors.Is(err, spaces.ErrSpaceNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			held.grants = append(held.grants, access.spacePermissions(uri, policy)...)
		}
	}
	allowed := make(map[permission]bool)
	for _, want := range wanted.grants {
		for _, grant := range held.grants {
			if intersection, ok := want.intersect(grant); ok {
				allowed[intersection] = true
			}
		}
	}
	scopes := narrowScopes(group, allowed)
	if slices.Contains(requested, "atproto") {
		scopes = append([]string{"atproto"}, scopes...)
	}
	return scopes, nil
}
