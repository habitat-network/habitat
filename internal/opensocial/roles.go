package opensocial

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"go.opentelemetry.io/otel/trace"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/telemetry"
)

var (
	ErrRoleNotFound            = errors.New("role not found")
	ErrCannotDeleteBuiltinRole = errors.New("admin and member roles cannot be deleted")
	ErrInvalidRoleID           = errors.New(
		"role ids are at least 3 lowercase letters and hyphens",
	)
	ErrRoleNotAssignable = errors.New(
		"caller may not assign or revoke one of the given roles",
	)
	ErrMemberNotFound = errors.New("member not found")
	ErrLastAdmin      = errors.New("a group cannot lose its last admin")
)

// AssignAll is the canAssign entry that covers every role, including roles
// created later.
const AssignAll = "*"

var roleIDPattern = regexp.MustCompile(`^[a-z-]{3,}$`)

// ValidRoleID reports whether id is a legal role id: label value syntax, at
// least three characters.
func ValidRoleID(id string) bool { return roleIDPattern.MatchString(id) }

// Role is a role record.
type Role = opensocial_api.OrgAtgrpRole

func membersSpaceOf(org syntax.DID) habitat_syntax.SpaceURI {
	return habitat_syntax.ConstructSpaceURI(org, MembersSpaceType, "self")
}

// GetRoles returns every role declared in orgDID, by id.
func (s *Store) GetRoles(ctx context.Context, orgDID syntax.DID) (map[string]Role, error) {
	collection := syntax.NSID(RoleCollection)
	records, err := s.spacesStore.ListRecords(ctx, membersSpaceOf(orgDID), orgDID, &collection)
	if err != nil {
		return nil, fmt.Errorf("list role records: %w", err)
	}
	roles := make(map[string]Role, len(records))
	for _, record := range records {
		var role Role
		if err := decodeRecordValue(record.Value, &role); err != nil {
			return nil, fmt.Errorf("decode role record %s: %w", record.Rkey, err)
		}
		roles[string(record.Rkey)] = role
	}
	return roles, nil
}

// listRoleRkeys returns the ids of every role declared in orgDID.
func (s *Store) listRoleRkeys(ctx context.Context, orgDID syntax.DID) ([]string, error) {
	roles, err := s.GetRoles(ctx, orgDID)
	if err != nil {
		return nil, err
	}
	return slices.Sorted(mapsKeys(roles)), nil
}

func mapsKeys[V any](m map[string]V) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// UserScopes returns the scopes user holds in orgDID: the union of the
// permissions of every role they hold. The group DID is implicitly a member
// with every scope.
func (s *Store) UserScopes(
	ctx context.Context,
	orgDID syntax.DID,
	user syntax.DID,
) (ScopeSet, error) {
	if user == orgDID {
		return AllScopes(), nil
	}
	held, err := s.GetUserRoles(ctx, orgDID, user)
	if err != nil {
		return ScopeSet{}, fmt.Errorf("get user roles: %w", err)
	}
	if len(held) == 0 {
		return ScopeSet{}, nil
	}
	roles, err := s.GetRoles(ctx, orgDID)
	if err != nil {
		return ScopeSet{}, err
	}
	var scopes [][]string
	for _, id := range held {
		scopes = append(scopes, roles[id].Permissions)
	}
	return NewScopeSet(scopes...), nil
}

// SpaceScopes is UserScopes plus the permissions the space's policy grants
// user's roles for acting as the group in that space.
func (s *Store) SpaceScopes(
	ctx context.Context,
	user syntax.DID,
	space habitat_syntax.SpaceURI,
) (ScopeSet, error) {
	orgDID := space.SpaceOwner()
	scopes, err := s.UserScopes(ctx, orgDID, user)
	if err != nil || scopes.all {
		return scopes, err
	}
	held, err := s.GetUserRoles(ctx, orgDID, user)
	if err != nil {
		return ScopeSet{}, fmt.Errorf("get user roles: %w", err)
	}
	policy, err := s.GetPolicy(ctx, space)
	if err != nil {
		return ScopeSet{}, err
	}
	for _, grant := range policy.Grants {
		if slices.Contains(held, grant.Role) {
			scopes.scopes = append(scopes.scopes, grant.Permissions...)
		}
	}
	return scopes, nil
}

// AssignSet is the roles a user may grant, revoke, or eject holders of: the
// union of canAssign across their roles.
type AssignSet struct {
	all   bool
	roles []string
}

// AssignAllRoles is the assign set of the group DID.
func AssignAllRoles() AssignSet { return AssignSet{all: true} }

// Has reports whether role is in the set.
func (a AssignSet) Has(role string) bool { return a.all || slices.Contains(a.roles, role) }

// HasAll reports whether the set holds the wildcard.
func (a AssignSet) HasAll() bool { return a.all }

// Roles returns the explicit roles in the set.
func (a AssignSet) Roles() []string { return a.roles }

// AssignableRoles returns the roles user may assign. The group DID, and any
// role holding `*`, may assign every role.
func (s *Store) AssignableRoles(
	ctx context.Context,
	orgDID syntax.DID,
	user syntax.DID,
) (_ AssignSet, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.AssignableRoles", trace.WithAttributes(
		telemetry.Org(orgDID), memberAttr(user),
	))
	defer func() { telemetry.End(span, err) }()
	if user == orgDID {
		return AssignAllRoles(), nil
	}
	held, err := s.GetUserRoles(ctx, orgDID, user)
	if err != nil {
		return AssignSet{}, fmt.Errorf("get user roles: %w", err)
	}
	if len(held) == 0 {
		return AssignSet{}, nil
	}
	roles, err := s.GetRoles(ctx, orgDID)
	if err != nil {
		return AssignSet{}, err
	}
	var set AssignSet
	for _, id := range held {
		for _, entry := range roles[id].CanAssign {
			if entry == AssignAll {
				set.all = true
			} else if !slices.Contains(set.roles, entry) {
				set.roles = append(set.roles, entry)
			}
		}
	}
	slices.Sort(set.roles)
	return set, nil
}

// CanAssignRoles reports whether user may move a member between their
// current roles and targetRoles: every role added or removed must be one
// user may assign.
func (s *Store) CanAssignRoles(
	ctx context.Context,
	orgDID syntax.DID,
	user syntax.DID,
	currentRoles []string,
	targetRoles []string,
) (ok bool, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.CanAssignRoles", trace.WithAttributes(
		telemetry.Org(orgDID), memberAttr(user), rolesKey.StringSlice(targetRoles),
	))
	defer func() {
		span.SetAttributes(allowedKey.Bool(ok))
		telemetry.End(span, err)
	}()
	assignable, err := s.AssignableRoles(ctx, orgDID, user)
	if err != nil {
		return false, fmt.Errorf("assignable roles: %w", err)
	}
	return assignable.coversChange(currentRoles, targetRoles), nil
}

// coversChange reports whether every role in the symmetric difference of the
// two lists is in the set.
func (a AssignSet) coversChange(current, target []string) bool {
	for _, role := range current {
		if !slices.Contains(target, role) && !a.Has(role) {
			return false
		}
	}
	for _, role := range target {
		if !slices.Contains(current, role) && !a.Has(role) {
			return false
		}
	}
	return true
}

// CheckAction reports whether user holds the scopes action requires in orgDID.
func (s *Store) CheckAction(
	ctx context.Context,
	orgDID syntax.DID,
	user syntax.DID,
	action Action,
) (ok bool, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.CheckAction", trace.WithAttributes(
		telemetry.Org(orgDID), memberAttr(user), actionKey.String(string(action)),
	))
	defer func() {
		span.SetAttributes(allowedKey.Bool(ok))
		telemetry.End(span, err)
	}()
	scopes, err := s.UserScopes(ctx, orgDID, user)
	if err != nil {
		return false, err
	}
	return scopes.CanPerform(action), nil
}

// PutRole writes a role record without any authorization check; callers
// enforce the escalation rule (see UpdateRole).
func (s *Store) PutRole(
	ctx context.Context,
	orgDID syntax.DID,
	rkey syntax.RecordKey,
	role Role,
) (err error) {
	ctx, span := tracer.Start(ctx, "opensocial.PutRole", trace.WithAttributes(
		telemetry.Org(orgDID), roleKey.String(rkey.String()),
	))
	defer func() { telemetry.End(span, err) }()
	if !ValidRoleID(string(rkey)) {
		return ErrInvalidRoleID
	}
	recordBytes, err := spaces.MarshalRecord(role)
	if err != nil {
		return fmt.Errorf("marshal role record: %w", err)
	}
	if _, _, err := s.spacesStore.PutRecord(
		ctx, membersSpaceOf(orgDID), orgDID, RoleCollection, rkey, recordBytes,
	); err != nil {
		return fmt.Errorf("put role record: %w", err)
	}
	return nil
}

// DeleteRole removes a role declaration. The built-in admin and member
// roles, seeded by NewOrg, may not be deleted.
func (s *Store) DeleteRole(
	ctx context.Context,
	orgDID syntax.DID,
	rkey syntax.RecordKey,
) (err error) {
	ctx, span := tracer.Start(ctx, "opensocial.DeleteRole", trace.WithAttributes(
		telemetry.Org(orgDID), roleKey.String(rkey.String()),
	))
	defer func() { telemetry.End(span, err) }()
	if string(rkey) == AdminRoleRkey || string(rkey) == MemberRoleRkey {
		return ErrCannotDeleteBuiltinRole
	}
	if _, err := s.spacesStore.GetRecord(
		ctx, membersSpaceOf(orgDID), orgDID, RoleCollection, rkey,
	); errors.Is(err, spaces.ErrRecordNotFound) {
		return ErrRoleNotFound
	} else if err != nil {
		return fmt.Errorf("get role record: %w", err)
	}
	if err := s.spacesStore.DeleteRecord(
		ctx, membersSpaceOf(orgDID), orgDID, RoleCollection, string(rkey),
	); err != nil {
		return fmt.Errorf("delete role record: %w", err)
	}
	return nil
}

// EjectMember removes a member from the group, revoking their roles (and so
// their access) by deleting their membership record.
func (s *Store) EjectMember(
	ctx context.Context,
	orgDID syntax.DID,
	member syntax.DID,
) (err error) {
	ctx, span := tracer.Start(ctx, "opensocial.EjectMember", trace.WithAttributes(
		telemetry.Org(orgDID), memberAttr(member),
	))
	defer func() { telemetry.End(span, err) }()
	if _, err := s.spacesStore.GetRecord(
		ctx, membersSpaceOf(orgDID), orgDID, MembershipCollection, syntax.RecordKey(member),
	); errors.Is(err, spaces.ErrRecordNotFound) {
		return ErrMemberNotFound
	} else if err != nil {
		return fmt.Errorf("get membership record: %w", err)
	}
	if err := s.keepsAdmin(ctx, orgDID, member, nil); err != nil {
		return err
	}
	if err := s.spacesStore.DeleteRecord(
		ctx, membersSpaceOf(orgDID), orgDID, MembershipCollection, string(member),
	); err != nil {
		return fmt.Errorf("delete membership record: %w", err)
	}
	return nil
}
