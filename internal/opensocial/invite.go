package opensocial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"gorm.io/gorm"
)

var (
	ErrInviteNotFound      = errors.New("invite not found")
	ErrInviteAlreadyExists = errors.New("invitee already has a pending invite")
	ErrAlreadyMember       = errors.New("invitee is already a member")
)

// Invite is a pending invite to join a community, tracked in the invites
// table. It has no repo record until the invitee accepts it.
type Invite struct {
	ID        string
	Org       syntax.DID
	Invitee   syntax.DID
	Roles     []string
	CreatedAt time.Time
}

// inviteRow is the gorm model backing the invites table.
type inviteRow struct {
	ID        string     `gorm:"primaryKey"`
	OrgID     syntax.DID `gorm:"not null;uniqueIndex:idx_invite_org_invitee,priority:1"`
	Invitee   syntax.DID `gorm:"not null;uniqueIndex:idx_invite_org_invitee,priority:2"`
	Roles     string     `gorm:"not null"` // JSON-encoded []string
	CreatedAt time.Time  `gorm:"not null"`
}

func (r inviteRow) toInvite() (Invite, error) {
	var roles []string
	if err := json.Unmarshal([]byte(r.Roles), &roles); err != nil {
		return Invite{}, fmt.Errorf("unmarshal roles: %w", err)
	}
	return Invite{
		ID:        r.ID,
		Org:       r.OrgID,
		Invitee:   r.Invitee,
		Roles:     roles,
		CreatedAt: r.CreatedAt,
	}, nil
}

func (s *Store) CreateInvite(
	ctx context.Context,
	orgDID syntax.DID,
	invitee syntax.DID,
	roles []string,
) (Invite, error) {
	existingRoles, err := s.GetUserRoles(ctx, orgDID, invitee)
	if err != nil {
		return Invite{}, fmt.Errorf("get user roles: %w", err)
	}
	if len(existingRoles) > 0 {
		return Invite{}, ErrAlreadyMember
	}
	rolesJSON, err := json.Marshal(roles)
	if err != nil {
		return Invite{}, fmt.Errorf("marshal roles: %w", err)
	}
	row := inviteRow{
		ID:        uuid.NewString(),
		OrgID:     orgDID,
		Invitee:   invitee,
		Roles:     string(rolesJSON),
		CreatedAt: time.Now(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return Invite{}, ErrInviteAlreadyExists
		}
		return Invite{}, fmt.Errorf("create invite: %w", err)
	}
	return row.toInvite()
}

// ListInvites returns invitee's pending invites across every org on this
// instance.
func (s *Store) ListInvites(ctx context.Context, invitee syntax.DID) ([]Invite, error) {
	var rows []inviteRow
	if err := s.db.WithContext(ctx).
		Where("invitee = ?", invitee).
		Order("created_at").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list my invites: %w", err)
	}
	return rowsToInvites(rows)
}

func (s *Store) ListPendingInvites(ctx context.Context, orgDID syntax.DID) ([]Invite, error) {
	var rows []inviteRow
	if err := s.db.WithContext(ctx).
		Where("org_id = ?", orgDID).
		Order("created_at").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list pending invites: %w", err)
	}
	return rowsToInvites(rows)
}

func rowsToInvites(rows []inviteRow) ([]Invite, error) {
	invites := make([]Invite, len(rows))
	for i, row := range rows {
		invite, err := row.toInvite()
		if err != nil {
			return nil, err
		}
		invites[i] = invite
	}
	return invites, nil
}

func (s *Store) RevokeInvite(ctx context.Context, orgDID syntax.DID, id string) error {
	result := s.db.WithContext(ctx).
		Where("org_id = ? AND id = ?", orgDID, id).
		Delete(&inviteRow{})
	if result.Error != nil {
		return fmt.Errorf("revoke invite: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrInviteNotFound
	}
	return nil
}

// RequestJoin consumes invitee's pending invite to orgDID, or — if they hold
// no pending invite but already have a membership (e.g. the org's creator,
// bootstrapped directly by NewOrg) — just confirms it, returning the roles
// granted. It does not write invitee's own acceptance record: that's the
// frontend's responsibility, done under invitee's own credentials after this
// returns, since it's their explicit act of joining and not something the
// backend should author on their behalf.
func (s *Store) RequestJoin(
	ctx context.Context,
	orgDID syntax.DID,
	invitee syntax.DID,
) ([]string, error) {
	var roles []string
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		spacesStoreTx := s.spacesStore.WithTx(tx)
		membersSpace := habitat_syntax.ConstructSpaceURI(
			orgDID, "community.opensocial.members", "self",
		)

		var row inviteRow
		err := tx.Where("org_id = ? AND invitee = ?", orgDID, invitee).First(&row).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			existingRoles, gerr := s.GetUserRoles(ctx, orgDID, invitee)
			if gerr != nil {
				return fmt.Errorf("get user roles: %w", gerr)
			}
			if len(existingRoles) == 0 {
				return ErrInviteNotFound
			}
			roles = existingRoles
		case err != nil:
			return fmt.Errorf("get invite: %w", err)
		default:
			if err := json.Unmarshal([]byte(row.Roles), &roles); err != nil {
				return fmt.Errorf("unmarshal roles: %w", err)
			}
			if err := tx.Delete(&row).Error; err != nil {
				return fmt.Errorf("delete invite: %w", err)
			}
			if _, _, err := spacesStoreTx.PutRecord(
				ctx, membersSpace, orgDID, "community.opensocial.membership",
				syntax.RecordKey(invitee),
				opensocial_api.CommunityOpensocialMembership{
					Roles:     roles,
					UpdatedAt: time.Now().Format(time.RFC3339),
				},
			); err != nil {
				return fmt.Errorf("put membership record: %w", err)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return roles, nil
}
