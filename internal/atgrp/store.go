package atgrp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"

	atgrp_api "github.com/habitat-network/habitat/api/atgrp"
	"github.com/habitat-network/habitat/api/comatproto"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	MembersSpaceType       = habitat_syntax.AtgrpMembersSpaceType
	RoleCollection         = "org.atgrp.role"
	MembershipCollection   = "org.atgrp.membership"
	ConfirmationCollection = "org.atgrp.confirmation"
	AdminRole              = "admin"
	MemberRole             = "member"
)

const adminSpacePermissions = "space:*?collection=*&manage=create&manage=update&manage=delete"

var (
	ErrInvalidRecord = errors.New("invalid Atmosphere Groups record")
	ErrLastAdmin     = errors.New("last group administrator")
)

// Store manages a group's role and membership records. It doesn't fall back
// to legacy opensocial records: callers must select the group model first.
type Store struct {
	db     *gorm.DB
	spaces spaces.Store
}

func NewStore(db *gorm.DB, store spaces.Store) *Store {
	return &Store{db: db, spaces: store}
}

func (s *Store) WithTx(tx *gorm.DB) *Store {
	return NewStore(tx, s.spaces.WithTx(tx))
}

// WithGroupTx serializes mutations that depend on group roles: role and
// membership writes, invite redemption, and group policy updates. Run all
// state reads and writes in fn on this transaction, not the original stores.
func (s *Store) WithGroupTx(ctx context.Context, group syntax.DID, fn func(*gorm.DB) error) error {
	return spaces.Transaction(ctx, s.db, func(tx *gorm.DB) error {
		if tx.Name() == "postgres" {
			// Don't take a space row lock before the backing store takes its
			// repo lock. Remote repo sync takes those locks in the other order.
			if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?), hashtext(?))`,
				"habitat:atgrp:transactions", group).Error; err != nil {
				return fmt.Errorf("lock group membership: %w", err)
			}
			exists, err := s.spaces.WithTx(tx).CheckSpaceExists(ctx, MembersSpace(group))
			if err != nil {
				return err
			}
			if !exists {
				return spaces.ErrSpaceNotFound
			}
			return fn(tx)
		}
		// Reserve the SQLite writer before taking a snapshot. Acquiring it
		// after reading would allow a stale snapshot to race a newer write.
		res := tx.Exec(`UPDATE spaces SET skey = skey WHERE owner = ? AND type = ? AND skey = ?`,
			group, MembersSpaceType, "self")
		if res.Error != nil {
			return fmt.Errorf("lock group membership: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return spaces.ErrSpaceNotFound
		}
		return fn(tx)
	})
}

func MembersSpace(group syntax.DID) habitat_syntax.SpaceURI {
	return habitat_syntax.ConstructSpaceURI(group, MembersSpaceType, "self")
}

var roleIDPattern = regexp.MustCompile(`^[a-z-]{3,512}$`)

func ValidRoleID(role string) bool {
	return roleIDPattern.MatchString(role)
}

// Bootstrap writes the default role records and initial group-authored
// membership. Provisioning calls this after creating the members space, on
// the transaction that creates the group. Only the creator can confirm.
func (s *Store) Bootstrap(ctx context.Context, group, creator syntax.DID) error {
	return s.WithGroupTx(ctx, group, func(tx *gorm.DB) error {
		return s.WithTx(tx).bootstrap(ctx, group, creator)
	})
}

func (s *Store) bootstrap(ctx context.Context, group, creator syntax.DID) error {
	if _, err := memberKey(group, creator); err != nil {
		return err
	}
	builtins := []struct {
		id   string
		role atgrp_api.OrgAtgrpRole
	}{
		{AdminRole, atgrp_api.OrgAtgrpRole{
			DisplayName: "Admin",
			CanAssign:   []string{wildcard},
			Permissions: []string{
				adminSpacePermissions,
				"include:org.atgrp.authManageMembers",
			},
		}},
		{MemberRole, atgrp_api.OrgAtgrpRole{DisplayName: "Member", Default: true}},
	}
	writes := make([]spaces.Write, 0, len(builtins)+1)
	for _, builtin := range builtins {
		value, err := spaces.MarshalRecord(builtin.role)
		if err != nil {
			return err
		}
		writes = append(writes, spaces.Write{
			Action: spaces.WriteCreate, Collection: RoleCollection,
			Rkey: syntax.RecordKey(builtin.id), Value: value,
		})
	}
	membership, err := newMembership([]string{AdminRole, MemberRole})
	if err != nil {
		return err
	}
	writes = append(writes, spaces.Write{
		Action: spaces.WriteCreate, Collection: MembershipCollection,
		Rkey: syntax.RecordKey(creator), Value: membership,
	})
	_, err = s.spaces.ApplyWrites(ctx, MembersSpace(group), group, writes)
	return err
}

type Membership struct {
	Roles     []string
	CreatedAt string
	Ref       comatproto.ComAtprotoRepoStrongRefMain
}

func memberKey(group, member syntax.DID) (syntax.RecordKey, error) {
	if member == group {
		return "", fmt.Errorf("%w: group is already an implicit member", ErrInvalidRecord)
	}
	if _, err := syntax.ParseDID(member.String()); err != nil {
		return "", fmt.Errorf("%w: invalid member DID: %w", ErrInvalidRecord, err)
	}
	key, err := syntax.ParseRecordKey(member.String())
	if err != nil {
		return "", fmt.Errorf("%w: invalid membership key: %w", ErrInvalidRecord, err)
	}
	return key, nil
}

func newMembership(roles []string) (spaces.MarshaledRecord, error) {
	return spaces.MarshalRecord(atgrp_api.OrgAtgrpMembership{
		Roles: append([]string{}, roles...), CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Store) GetMembership(ctx context.Context, group, member syntax.DID) (Membership, error) {
	key, err := memberKey(group, member)
	if err != nil {
		return Membership{}, err
	}
	space := MembersSpace(group)
	record, err := s.spaces.GetRecord(ctx, space, group, MembershipCollection, key)
	if err != nil {
		return Membership{}, err
	}
	return readMembership(space, record)
}

func readMembership(space habitat_syntax.SpaceURI, record *spaces.Record) (Membership, error) {
	var membership atgrp_api.OrgAtgrpMembership
	if err := decodeRecord(record.Value, MembershipCollection, &membership); err != nil {
		return Membership{}, err
	}
	if err := validateMembership(membership); err != nil {
		return Membership{}, err
	}
	uri := habitat_syntax.ConstructSpaceRecordURI(
		space,
		space.SpaceOwner(),
		record.Collection,
		record.Rkey,
	)
	return Membership{
		Roles:     membership.Roles,
		CreatedAt: membership.CreatedAt,
		Ref: comatproto.ComAtprotoRepoStrongRefMain{
			Uri: uri.AtSpace().String(),
			Cid: record.Cid.String(),
		},
	}, nil
}

// IsConfirmedMember reports whether both sides of membership are present in
// the local space repo view. Use this for roster display, not authorization.
func (s *Store) IsConfirmedMember(ctx context.Context, group, member syntax.DID) (bool, error) {
	if _, err := s.GetMembership(ctx, group, member); errors.Is(err, spaces.ErrRecordNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	record, err := s.spaces.GetRecord(
		ctx,
		MembersSpace(group),
		member,
		ConfirmationCollection,
		"self",
	)
	if errors.Is(err, spaces.ErrRecordNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	var confirmation atgrp_api.OrgAtgrpConfirmation
	if err := decodeRecord(record.Value, ConfirmationCollection, &confirmation); err != nil {
		return false, err
	}
	if _, err := syntax.ParseDatetime(confirmation.CreatedAt); err != nil {
		return false, fmt.Errorf("%w: invalid confirmation createdAt: %w", ErrInvalidRecord, err)
	}
	return true, nil
}

// CreateMembership admits someone after a trusted invite redemption. Call
// within WithGroupTx alongside the redeem check. It never changes existing
// membership roles and never authors a confirmation on the member's behalf.
func (s *Store) CreateMembership(
	ctx context.Context, group, member syntax.DID, roleIDs []string,
) (ref comatproto.ComAtprotoRepoStrongRefMain, err error) {
	err = s.WithGroupTx(ctx, group, func(tx *gorm.DB) error {
		ref, err = s.WithTx(tx).createMembership(ctx, group, member, roleIDs)
		return err
	})
	if err != nil {
		return comatproto.ComAtprotoRepoStrongRefMain{}, err
	}
	return ref, nil
}

func (s *Store) createMembership(
	ctx context.Context, group, member syntax.DID, roleIDs []string,
) (comatproto.ComAtprotoRepoStrongRefMain, error) {
	var empty comatproto.ComAtprotoRepoStrongRefMain
	key, err := memberKey(group, member)
	if err != nil {
		return empty, err
	}
	if err := validateRoleIDs(roleIDs); err != nil {
		return empty, err
	}
	for _, role := range roleIDs {
		if _, err := s.getRole(ctx, group, role); err != nil {
			return empty, fmt.Errorf("get admitted role %q: %w", role, err)
		}
	}
	value, err := newMembership(roleIDs)
	if err != nil {
		return empty, err
	}
	results, err := s.spaces.ApplyWrites(ctx, MembersSpace(group), group, []spaces.Write{{
		Action: spaces.WriteCreate, Collection: MembershipCollection, Rkey: key, Value: value,
	}})
	if err != nil {
		return empty, err
	}
	return comatproto.ComAtprotoRepoStrongRefMain{
		Uri: results[0].URI.AtSpace().String(), Cid: results[0].Cid.String(),
	}, nil
}

// DeleteMembership is the trusted leave mutation. Call within WithGroupTx,
// and reconcile writer sets before committing. An absent membership is a
// no-op. The member owns their own confirmation, which is not changed.
func (s *Store) DeleteMembership(ctx context.Context, group, member syntax.DID) error {
	return s.WithGroupTx(ctx, group, func(tx *gorm.DB) error {
		return s.WithTx(tx).deleteMembership(ctx, group, member)
	})
}

func (s *Store) deleteMembership(ctx context.Context, group, member syntax.DID) error {
	membership, err := s.GetMembership(ctx, group, member)
	if errors.Is(err, spaces.ErrRecordNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	actor, err := s.loadRoles(ctx, group, membership.Roles)
	if err != nil {
		return err
	}
	members := MembersSpace(group)
	policy, err := s.spaces.GetSpacePolicy(ctx, members)
	if err != nil {
		return err
	}
	if actor.canRecover(members, policy) {
		ok, err := s.hasRecoveryMember(ctx, group, member, policy)
		if err != nil {
			return err
		}
		if !ok {
			return ErrLastAdmin
		}
	}
	return s.spaces.DeleteRecord(
		ctx,
		members,
		group,
		MembershipCollection,
		member.String(),
	)
}

// DefaultRoles returns the role IDs declared as the invite-only admission
// defaults. An empty list is still a valid membership record.
func (s *Store) DefaultRoles(ctx context.Context, group syntax.DID) ([]string, error) {
	collection := syntax.NSID(RoleCollection)
	records, err := s.spaces.ListRecords(ctx, MembersSpace(group), group, &collection)
	if err != nil {
		return nil, err
	}
	roles := make([]string, 0)
	for _, record := range records {
		role, err := readRole(&record)
		if err != nil {
			return nil, err
		}
		if role.Default {
			roles = append(roles, record.Rkey.String())
		}
	}
	slices.Sort(roles)
	if len(roles) > 100 {
		return nil, fmt.Errorf("%w: too many default roles", ErrInvalidRecord)
	}
	return roles, nil
}

func (s *Store) getRole(
	ctx context.Context,
	group syntax.DID,
	role string,
) (atgrp_api.OrgAtgrpRole, error) {
	record, err := s.spaces.GetRecord(
		ctx,
		MembersSpace(group),
		group,
		RoleCollection,
		syntax.RecordKey(role),
	)
	if err != nil {
		return atgrp_api.OrgAtgrpRole{}, err
	}
	return readRole(record)
}

func readRole(record *spaces.Record) (atgrp_api.OrgAtgrpRole, error) {
	var role atgrp_api.OrgAtgrpRole
	if !ValidRoleID(record.Rkey.String()) {
		return role, fmt.Errorf("%w: invalid role ID %q", ErrInvalidRecord, record.Rkey)
	}
	if err := decodeRecord(record.Value, RoleCollection, &role); err != nil {
		return role, err
	}
	return role, nil
}

func decodeRecord(value map[string]any, collection string, out any) error {
	if value["$type"] != collection {
		return fmt.Errorf("%w: expected %s", ErrInvalidRecord, collection)
	}
	record, err := json.Marshal(value)
	if err == nil {
		err = json.Unmarshal(record, out)
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidRecord, err)
	}
	return nil
}

func validateMembership(membership atgrp_api.OrgAtgrpMembership) error {
	if membership.Roles == nil {
		return fmt.Errorf("%w: missing roles", ErrInvalidRecord)
	}
	if _, err := syntax.ParseDatetime(membership.CreatedAt); err != nil {
		return fmt.Errorf("%w: invalid membership createdAt: %w", ErrInvalidRecord, err)
	}
	return validateRoleIDs(membership.Roles)
}

func validateRoleIDs(roles []string) error {
	if len(roles) > 100 {
		return fmt.Errorf("%w: too many roles", ErrInvalidRecord)
	}
	seen := make(map[string]bool, len(roles))
	for _, role := range roles {
		if !ValidRoleID(role) || seen[role] {
			return fmt.Errorf("%w: invalid or duplicate role %q", ErrInvalidRecord, role)
		}
		seen[role] = true
	}
	return nil
}

func (s *Store) hasRecoveryMember(
	ctx context.Context, group, except syntax.DID, policy spaces.Policy,
) (bool, error) {
	collection := syntax.NSID(MembershipCollection)
	space := MembersSpace(group)
	records, err := s.spaces.ListRecords(ctx, space, group, &collection)
	if err != nil {
		return false, err
	}
	for _, record := range records {
		member := syntax.DID(record.Rkey)
		if member == except || member == group {
			continue
		}
		if _, err := memberKey(group, member); err != nil {
			return false, err
		}
		membership, err := readMembership(space, &record)
		if err != nil {
			return false, err
		}
		access, err := s.loadRoles(ctx, group, membership.Roles)
		if err != nil {
			return false, err
		}
		if access.canRecover(space, policy) {
			return true, nil
		}
	}
	return false, nil
}
