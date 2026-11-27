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

	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/telemetry"
)

var (
	ErrInviteNotFound      = errors.New("invite not found")
	ErrInviteAlreadyExists = errors.New("invitee already has a pending invite")
	ErrAlreadyMember       = errors.New("invitee is already a member")
)

// Invite is a pending invite to join a group: a group.opensocial.invite
// record the group wrote into the invitee's invites space.
type Invite struct {
	ID        string
	Org       syntax.DID
	Invitee   syntax.DID
	Roles     []string
	CreatedAt time.Time
	ExpiresAt time.Time
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

// InviteCollection is the collection invites are written to.
const InviteCollection = "group.opensocial.invite"

// invitesSpace returns the space holding invitee's invites.
func invitesSpace(invitee syntax.DID) habitat_syntax.SpaceURI {
	return habitat_syntax.ConstructSpaceURI(invitee, InvitesSpaceType, "self")
}

// inviteFromRecord reads an invite record written by org into invitee's
// invites space. Roles are a Habitat extension to the spec's invite record: a
// spec reader ignores them, and an invite without them admits its recipient
// with the group's default roles.
func inviteFromRecord(record spaces.Record) Invite {
	invite := Invite{
		ID:      string(record.Rkey),
		Org:     record.Owner,
		Invitee: record.Space.SpaceOwner(),
	}
	if raw, ok := record.Value["roles"].([]any); ok {
		for _, role := range raw {
			if name, ok := role.(string); ok {
				invite.Roles = append(invite.Roles, name)
			}
		}
	}
	if created, ok := record.Value["createdAt"].(string); ok {
		invite.CreatedAt, _ = time.Parse(time.RFC3339, created)
	}
	if expires, ok := record.Value["expiresAt"].(string); ok {
		invite.ExpiresAt, _ = time.Parse(time.RFC3339, expires)
	}
	return invite
}

func (i Invite) expired() bool {
	return !i.ExpiresAt.IsZero() && time.Now().After(i.ExpiresAt)
}

// CreateInvite writes an invite, authored by orgDID, into invitee's invites
// space on this host, creating that space if it does not exist yet.
func (s *Store) CreateInvite(
	ctx context.Context,
	orgDID syntax.DID,
	invitee syntax.DID,
	roles []string,
) (_ Invite, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.CreateInvite", trace.WithAttributes(
		telemetry.Org(orgDID), memberAttr(invitee), rolesKey.StringSlice(roles),
	))
	defer func() { telemetry.End(span, err) }()
	existingRoles, err := s.GetUserRoles(ctx, orgDID, invitee)
	if err != nil {
		return Invite{}, fmt.Errorf("get user roles: %w", err)
	}
	if len(existingRoles) > 0 {
		return Invite{}, ErrAlreadyMember
	}
	space := invitesSpace(invitee)
	pending, err := s.pendingInvitesFor(ctx, orgDID, invitee)
	if err != nil {
		return Invite{}, err
	}
	if len(pending) > 0 {
		return Invite{}, ErrInviteAlreadyExists
	}
	exists, err := s.spacesStore.CheckSpaceExists(ctx, space)
	if err != nil {
		return Invite{}, fmt.Errorf("check invites space: %w", err)
	}
	if !exists {
		if _, err := s.spacesStore.CreateSpace(ctx, invitee, InvitesSpaceType, "self"); err != nil &&
			!errors.Is(err, spaces.ErrSpaceAlreadyExists) {
			return Invite{}, fmt.Errorf("create invites space: %w", err)
		}
	}
	now := time.Now().UTC()
	record := map[string]any{
		"$type":     InviteCollection,
		"createdAt": now.Format(time.RFC3339),
		"roles":     roles,
	}
	recordBytes, err := spaces.MarshalRecord(record)
	if err != nil {
		return Invite{}, fmt.Errorf("marshal invite record: %w", err)
	}
	rkey := syntax.NewTIDNow(0)
	if _, _, err := s.spacesStore.PutRecord(
		ctx, space, orgDID, InviteCollection, syntax.RecordKey(rkey.String()), recordBytes,
	); err != nil {
		return Invite{}, fmt.Errorf("put invite record: %w", err)
	}
	return Invite{
		ID: rkey.String(), Org: orgDID, Invitee: invitee, Roles: roles, CreatedAt: now,
	}, nil
}

// pendingInvitesFor returns the unexpired invites orgDID has written to invitee.
func (s *Store) pendingInvitesFor(
	ctx context.Context,
	orgDID, invitee syntax.DID,
) ([]Invite, error) {
	records, err := s.spacesStore.ListRecords(
		ctx, invitesSpace(invitee), orgDID, ptr(syntax.NSID(InviteCollection)),
	)
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	var invites []Invite
	for _, record := range records {
		record.Space = invitesSpace(invitee)
		if invite := inviteFromRecord(record); !invite.expired() {
			invites = append(invites, invite)
		}
	}
	return invites, nil
}

func ptr[T any](v T) *T { return &v }

// ListInvites returns invitee's pending invites from every group on this host.
func (s *Store) ListInvites(ctx context.Context, invitee syntax.DID) (_ []Invite, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.ListInvites", trace.WithAttributes(
		memberAttr(invitee),
	))
	defer func() { telemetry.End(span, err) }()
	records, err := s.spacesStore.ListCollection(
		ctx, invitesSpace(invitee), InviteCollection,
	)
	if err != nil {
		return nil, fmt.Errorf("list my invites: %w", err)
	}
	return invitesFromRecords(records), nil
}

// ListPendingInvites returns the invites orgDID has written, to anyone.
func (s *Store) ListPendingInvites(ctx context.Context, orgDID syntax.DID) (_ []Invite, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.ListPendingInvites", trace.WithAttributes(
		telemetry.Org(orgDID),
	))
	defer func() { telemetry.End(span, err) }()
	records, err := s.spacesStore.ListRepoCollection(
		ctx, orgDID, InvitesSpaceType, InviteCollection,
	)
	if err != nil {
		return nil, fmt.Errorf("list pending invites: %w", err)
	}
	return invitesFromRecords(records), nil
}

func invitesFromRecords(records []spaces.Record) []Invite {
	invites := make([]Invite, 0, len(records))
	for _, record := range records {
		if invite := inviteFromRecord(record); !invite.expired() {
			invites = append(invites, invite)
		}
	}
	slices.SortFunc(invites, func(a, b Invite) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return invites
}

// RevokeInvite deletes the invite record orgDID wrote with the given id.
func (s *Store) RevokeInvite(ctx context.Context, orgDID syntax.DID, id string) (err error) {
	ctx, span := tracer.Start(ctx, "opensocial.RevokeInvite", trace.WithAttributes(
		telemetry.Org(orgDID), inviteIDKey.String(id),
	))
	defer func() { telemetry.End(span, err) }()
	records, err := s.spacesStore.ListRepoCollection(
		ctx, orgDID, InvitesSpaceType, InviteCollection,
	)
	if err != nil {
		return fmt.Errorf("list invites: %w", err)
	}
	for _, record := range records {
		if string(record.Rkey) != id {
			continue
		}
		if err := s.spacesStore.DeleteRecord(
			ctx, record.Space, orgDID, InviteCollection, id,
		); err != nil {
			return fmt.Errorf("revoke invite: %w", err)
		}
		return nil
	}
	return ErrInviteNotFound
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
) (_ []string, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.RequestJoin", trace.WithAttributes(
		telemetry.Org(orgDID), memberAttr(invitee),
	))
	defer func() { telemetry.End(span, err) }()
	pending, err := s.pendingInvitesFor(ctx, orgDID, invitee)
	if err != nil {
		return nil, err
	}
	if len(pending) == 0 {
		existingRoles, err := s.GetUserRoles(ctx, orgDID, invitee)
		if err != nil {
			return nil, fmt.Errorf("get user roles: %w", err)
		}
		if len(existingRoles) == 0 {
			return nil, ErrInviteNotFound
		}
		return existingRoles, nil
	}
	invite := pending[0]
	roles := invite.Roles
	if len(roles) == 0 {
		if roles, err = s.DefaultRoles(ctx, orgDID); err != nil {
			return nil, err
		}
	}
	if err := putMembership(ctx, s.spacesStore, orgDID, invitee, roles); err != nil {
		return nil, err
	}
	if err := s.spacesStore.DeleteRecord(
		ctx, invitesSpace(invitee), orgDID, InviteCollection, invite.ID,
	); err != nil {
		return nil, fmt.Errorf("consume invite: %w", err)
	}
	return roles, nil
}
