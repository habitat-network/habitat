package opensocial

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	// ErrJoinRequestNotFound is returned when no pending request exists.
	ErrJoinRequestNotFound = errors.New("no pending join request")
	// ErrInvalidCursor is returned for a listJoinRequests cursor that was not
	// issued by this host.
	ErrInvalidCursor = errors.New("invalid cursor")
)

// JoinRequest is a pending request to join a group whose join policy is
// approval. The host keeps these privately: approving one means writing the
// requester's membership record, and denying one deletes it.
type JoinRequest struct {
	Member    syntax.DID
	Message   string
	CreatedAt time.Time
}

// joinRequestRow is the gorm model backing JoinRequest.
type joinRequestRow struct {
	OrgID     syntax.DID `gorm:"primaryKey"`
	Member    syntax.DID `gorm:"primaryKey"`
	Message   string     `gorm:"not null"`
	CreatedAt time.Time  `gorm:"not null"`
}

func (joinRequestRow) TableName() string { return "join_requests" }

func (r joinRequestRow) request() JoinRequest {
	return JoinRequest{Member: r.Member, Message: r.Message, CreatedAt: r.CreatedAt}
}

// QueueJoinRequest records member's request to join orgDID and returns it.
// Asking again returns the existing request unchanged.
func (s *Store) QueueJoinRequest(
	ctx context.Context,
	orgDID, member syntax.DID,
	message string,
) (JoinRequest, error) {
	row := joinRequestRow{
		OrgID: orgDID, Member: member, Message: message, CreatedAt: time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return JoinRequest{}, fmt.Errorf("queue join request: %w", err)
	}
	var stored joinRequestRow
	if err := s.db.WithContext(ctx).
		First(&stored, "org_id = ? AND member = ?", orgDID, member).Error; err != nil {
		return JoinRequest{}, fmt.Errorf("get join request: %w", err)
	}
	return stored.request(), nil
}

// GetJoinRequest returns member's pending request to join orgDID, or
// ErrJoinRequestNotFound.
func (s *Store) GetJoinRequest(
	ctx context.Context,
	orgDID, member syntax.DID,
) (JoinRequest, error) {
	var row joinRequestRow
	err := s.db.WithContext(ctx).First(&row, "org_id = ? AND member = ?", orgDID, member).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return JoinRequest{}, ErrJoinRequestNotFound
	}
	if err != nil {
		return JoinRequest{}, fmt.Errorf("get join request: %w", err)
	}
	return row.request(), nil
}

// DeleteJoinRequest removes member's pending request to join orgDID, or
// returns ErrJoinRequestNotFound.
func (s *Store) DeleteJoinRequest(ctx context.Context, orgDID, member syntax.DID) error {
	res := s.db.WithContext(ctx).
		Delete(&joinRequestRow{}, "org_id = ? AND member = ?", orgDID, member)
	if res.Error != nil {
		return fmt.Errorf("delete join request: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrJoinRequestNotFound
	}
	return nil
}

// ListJoinRequests lists orgDID's pending requests, oldest first, after
// cursor, and returns the cursor of the next page when there is one. A request
// whose author has been admitted since (an admin wrote their membership
// record) is dropped rather than listed.
func (s *Store) ListJoinRequests(
	ctx context.Context,
	orgDID syntax.DID,
	limit int,
	cursor string,
) (_ []JoinRequest, next string, err error) {
	query := s.db.WithContext(ctx).Where("org_id = ?", orgDID).
		Order("created_at, member").Limit(limit + 1)
	if cursor != "" {
		nanos, member, ok := strings.Cut(cursor, ":")
		parsed, parseErr := strconv.ParseInt(nanos, 10, 64)
		if !ok || parseErr != nil {
			return nil, "", ErrInvalidCursor
		}
		after := time.Unix(0, parsed).UTC()
		query = query.Where("created_at > ? OR (created_at = ? AND member > ?)",
			after, after, member)
	}
	var rows []joinRequestRow
	if err := query.Find(&rows).Error; err != nil {
		return nil, "", fmt.Errorf("list join requests: %w", err)
	}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next = fmt.Sprintf("%d:%s", last.CreatedAt.UnixNano(), last.Member)
	}
	requests := make([]JoinRequest, 0, len(rows))
	for _, row := range rows {
		roles, err := s.GetUserRoles(ctx, orgDID, row.Member)
		if err != nil {
			return nil, "", err
		}
		if len(roles) > 0 {
			if err := s.DeleteJoinRequest(ctx, orgDID, row.Member); err != nil &&
				!errors.Is(err, ErrJoinRequestNotFound) {
				return nil, "", err
			}
			continue
		}
		requests = append(requests, row.request())
	}
	return requests, next, nil
}
