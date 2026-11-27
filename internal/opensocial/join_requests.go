package opensocial

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/telemetry"
)

var ErrJoinRequestNotFound = errors.New("join request not found")

// JoinRequest is a pending request to join a group that approves its joins.
// The host stores these privately: they are not records in a space. Writing the
// requester's membership approves a request.
type JoinRequest struct {
	Group     syntax.DID
	Member    syntax.DID
	Message   string
	CreatedAt time.Time
}

// joinRequestRow is the gorm model backing the join_request_rows table.
type joinRequestRow struct {
	OrgID     syntax.DID `gorm:"primaryKey"`
	Member    syntax.DID `gorm:"primaryKey"`
	Message   string     `gorm:"not null"`
	CreatedAt time.Time  `gorm:"not null"`
}

func (r joinRequestRow) toRequest() JoinRequest {
	return JoinRequest{
		Group: r.OrgID, Member: r.Member, Message: r.Message, CreatedAt: r.CreatedAt,
	}
}

// CreateJoinRequest queues member's request to join orgDID. A second request
// from the same member replaces the first message but keeps its place.
func (s *Store) CreateJoinRequest(
	ctx context.Context,
	orgDID, member syntax.DID,
	message string,
) (_ JoinRequest, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.CreateJoinRequest", trace.WithAttributes(
		telemetry.Org(orgDID), memberAttr(member),
	))
	defer func() { telemetry.End(span, err) }()
	var row joinRequestRow
	err = s.db.WithContext(ctx).
		Where("org_id = ? AND member = ?", orgDID, member).First(&row).Error
	switch {
	case err == nil:
		row.Message = message
		if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
			return JoinRequest{}, fmt.Errorf("update join request: %w", err)
		}
	case errors.Is(err, gorm.ErrRecordNotFound):
		row = joinRequestRow{
			OrgID: orgDID, Member: member, Message: message, CreatedAt: time.Now().UTC(),
		}
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			return JoinRequest{}, fmt.Errorf("create join request: %w", err)
		}
	default:
		return JoinRequest{}, fmt.Errorf("get join request: %w", err)
	}
	return row.toRequest(), nil
}

// ListJoinRequests returns orgDID's pending requests, oldest first, after the
// cursor (the CreatedAt-and-member position of the last request seen). A
// request whose member already holds roles is stale — someone wrote their
// membership directly — and is dropped.
func (s *Store) ListJoinRequests(
	ctx context.Context,
	orgDID syntax.DID,
	limit int,
	after *JoinRequest,
) (_ []JoinRequest, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.ListJoinRequests", trace.WithAttributes(
		telemetry.Org(orgDID),
	))
	defer func() { telemetry.End(span, err) }()
	query := s.db.WithContext(ctx).Where("org_id = ?", orgDID)
	if after != nil {
		query = query.Where(
			"created_at > ? OR (created_at = ? AND member > ?)",
			after.CreatedAt, after.CreatedAt, after.Member,
		)
	}
	var rows []joinRequestRow
	if err := query.Order("created_at, member").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list join requests: %w", err)
	}
	var requests []JoinRequest
	for _, row := range rows {
		roles, err := s.GetUserRoles(ctx, orgDID, row.Member)
		if err != nil {
			return nil, fmt.Errorf("get user roles: %w", err)
		}
		if len(roles) > 0 {
			continue
		}
		requests = append(requests, row.toRequest())
	}
	return requests, nil
}

// DeleteJoinRequest removes member's request to join orgDID: they cancelled
// it, or the group denied it. It returns ErrJoinRequestNotFound if there is none.
func (s *Store) DeleteJoinRequest(
	ctx context.Context,
	orgDID, member syntax.DID,
) (err error) {
	ctx, span := tracer.Start(ctx, "opensocial.DeleteJoinRequest", trace.WithAttributes(
		telemetry.Org(orgDID), memberAttr(member),
	))
	defer func() { telemetry.End(span, err) }()
	result := s.db.WithContext(ctx).
		Where("org_id = ? AND member = ?", orgDID, member).Delete(&joinRequestRow{})
	if result.Error != nil {
		return fmt.Errorf("delete join request: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrJoinRequestNotFound
	}
	return nil
}

// clearJoinRequest drops member's request once they are admitted, ignoring
// that there may be none.
func (s *Store) clearJoinRequest(ctx context.Context, orgDID, member syntax.DID) error {
	if err := s.db.WithContext(ctx).
		Where("org_id = ? AND member = ?", orgDID, member).
		Delete(&joinRequestRow{}).Error; err != nil {
		return fmt.Errorf("clear join request: %w", err)
	}
	return nil
}
