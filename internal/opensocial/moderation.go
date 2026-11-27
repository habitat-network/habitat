package opensocial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/telemetry"
)

var (
	ErrInvalidSubject  = errors.New("subject is not in this group")
	ErrSubjectNotFound = errors.New("subject has never been reported")
)

// SubjectStatus is where a subject stands in the moderation queue.
type SubjectStatus string

const (
	StatusOpen      SubjectStatus = "open"
	StatusEscalated SubjectStatus = "escalated"
	StatusResolved  SubjectStatus = "resolved"
)

// EventKind says what happened in a ModerationEvent.
type EventKind string

const (
	EventReport EventKind = "report"
	EventStatus EventKind = "status"
	EventLabel  EventKind = "label"
	EventEject  EventKind = "eject"
)

// Subject is an account (URI is empty) or a record (URI set, CID optional).
type Subject struct {
	DID syntax.DID
	URI string
	CID string
}

// key identifies the subject in the group's queue: an account DID or a record URI.
func (s Subject) Key() string {
	if s.URI != "" {
		return s.URI
	}
	return string(s.DID)
}

// SubjectView is a subject in the moderation queue.
type SubjectView struct {
	Subject     Subject
	Status      SubjectStatus
	ReportCount int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Report is the reporter's input to CreateReport.
type Report struct {
	ReasonType string
	Reason     string
	Rule       string
}

// ModerationEvent is one entry in a subject's history. Payload is the
// kind-specific JSON object.
type ModerationEvent struct {
	ID        string
	Kind      EventKind
	CreatedAt time.Time
	CreatedBy syntax.DID
	Payload   json.RawMessage
}

// moderationSubjectRow is the gorm model backing moderation_subject_rows.
type moderationSubjectRow struct {
	OrgID       syntax.DID `gorm:"primaryKey;index:idx_moderation_subject_status,priority:1"`
	Subject     string     `gorm:"primaryKey"`
	CID         string     `gorm:"primaryKey;column:cid"`
	Status      string     `gorm:"not null;index:idx_moderation_subject_status,priority:2"`
	ReportCount int64      `gorm:"not null"`
	CreatedAt   time.Time  `gorm:"not null"`
	UpdatedAt   time.Time  `gorm:"not null;index:idx_moderation_subject_status,priority:3"`
}

// moderationEventRow is the gorm model backing moderation_event_rows.
type moderationEventRow struct {
	ID        string     `gorm:"primaryKey"`
	OrgID     syntax.DID `gorm:"not null;index:idx_moderation_event_subject,priority:1"`
	Subject   string     `gorm:"not null;index:idx_moderation_event_subject,priority:2"`
	CID       string     `gorm:"not null;column:cid"`
	Kind      string     `gorm:"not null"`
	CreatedBy syntax.DID `gorm:"not null"`
	Payload   string     `gorm:"not null"`
	CreatedAt time.Time  `gorm:"not null"`
}

func (r moderationSubjectRow) view() SubjectView {
	subject := Subject{CID: r.CID}
	if strings.HasPrefix(r.Subject, "did:") {
		subject.DID = syntax.DID(r.Subject)
	} else {
		subject.URI = r.Subject
	}
	return SubjectView{
		Subject: subject, Status: SubjectStatus(r.Status), ReportCount: r.ReportCount,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// validateSubject checks the subject is in the group: an account must hold a
// membership, and a record must live in a space the group owns.
func (s *Store) validateSubject(ctx context.Context, group syntax.DID, subject Subject) error {
	if subject.URI == "" {
		roles, err := s.GetUserRoles(ctx, group, subject.DID)
		if err != nil {
			return fmt.Errorf("get user roles: %w", err)
		}
		if len(roles) == 0 {
			return ErrInvalidSubject
		}
		return nil
	}
	uri, err := habitat_syntax.ParseSpaceRecordURI(subject.URI)
	if err != nil || uri.SpaceOwner() != group {
		return ErrInvalidSubject
	}
	return nil
}

// recordEvent appends an event to the subject's history and updates its queue
// row. A report on a resolved subject reopens it.
func (s *Store) recordEvent(
	ctx context.Context,
	group syntax.DID,
	subject Subject,
	kind EventKind,
	actor syntax.DID,
	payload any,
	newStatus SubjectStatus,
) (SubjectView, ModerationEvent, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return SubjectView{}, ModerationEvent{}, fmt.Errorf("marshal event: %w", err)
	}
	now := time.Now().UTC()
	event := moderationEventRow{
		ID: syntax.NewTIDNow(0).String(), OrgID: group, Subject: subject.Key(), CID: subject.CID,
		Kind: string(kind), CreatedBy: actor, Payload: string(raw), CreatedAt: now,
	}
	var view SubjectView
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&event).Error; err != nil {
			return fmt.Errorf("create event: %w", err)
		}
		row := moderationSubjectRow{
			OrgID: group, Subject: subject.Key(), CID: subject.CID,
			Status: string(StatusOpen), CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return fmt.Errorf("create subject: %w", err)
		}
		if err := tx.Where(
			"org_id = ? AND subject = ? AND cid = ?", group, subject.Key(), subject.CID,
		).First(&row).Error; err != nil {
			return fmt.Errorf("get subject: %w", err)
		}
		if kind == EventReport {
			row.ReportCount++
		}
		if newStatus != "" {
			row.Status = string(newStatus)
		}
		row.UpdatedAt = now
		if err := tx.Model(&moderationSubjectRow{}).Where(
			"org_id = ? AND subject = ? AND cid = ?", group, subject.Key(), subject.CID,
		).Updates(map[string]any{
			"status": row.Status, "report_count": row.ReportCount, "updated_at": now,
		}).Error; err != nil {
			return fmt.Errorf("update subject: %w", err)
		}
		view = row.view()
		return nil
	})
	if err != nil {
		return SubjectView{}, ModerationEvent{}, err
	}
	return view, event.toEvent(), nil
}

func (r moderationEventRow) toEvent() ModerationEvent {
	return ModerationEvent{
		ID: r.ID, Kind: EventKind(r.Kind), CreatedAt: r.CreatedAt, CreatedBy: r.CreatedBy,
		Payload: json.RawMessage(r.Payload),
	}
}

// CreateReport files a report on subject against group. The subject must be in
// the group, and reporting reopens a resolved subject.
func (s *Store) CreateReport(
	ctx context.Context,
	group, reporter syntax.DID,
	subject Subject,
	report Report,
) (_ ModerationEvent, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.CreateReport", trace.WithAttributes(
		telemetry.Org(group),
	))
	defer func() { telemetry.End(span, err) }()
	if err := s.validateSubject(ctx, group, subject); err != nil {
		return ModerationEvent{}, err
	}
	_, event, err := s.recordEvent(
		ctx, group, subject, EventReport, reporter, report, StatusOpen,
	)
	return event, err
}

// ResolveSubject sets a subject's status (resolved or escalated) with an
// optional note. It returns ErrSubjectNotFound for a subject never reported.
func (s *Store) ResolveSubject(
	ctx context.Context,
	group, moderator syntax.DID,
	subject Subject,
	status SubjectStatus,
	note string,
) (_ SubjectView, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.ResolveSubject", trace.WithAttributes(
		telemetry.Org(group),
	))
	defer func() { telemetry.End(span, err) }()
	if err := s.validateSubject(ctx, group, subject); err != nil {
		return SubjectView{}, err
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&moderationSubjectRow{}).Where(
		"org_id = ? AND subject = ? AND cid = ?", group, subject.Key(), subject.CID,
	).Count(&count).Error; err != nil {
		return SubjectView{}, fmt.Errorf("count subject: %w", err)
	}
	if count == 0 {
		return SubjectView{}, ErrSubjectNotFound
	}
	view, _, err := s.recordEvent(
		ctx, group, subject, EventStatus, moderator,
		map[string]string{"status": string(status), "note": note}, status,
	)
	return view, err
}

// RecordEject notes in the subject's history that the group ejected it.
func (s *Store) RecordEject(ctx context.Context, group, moderator, member syntax.DID) error {
	_, _, err := s.recordEvent(
		ctx, group, Subject{DID: member}, EventEject, moderator, map[string]string{}, "",
	)
	return err
}

// RecordLabel notes in the subject's history that a label record was written
// or deleted.
func (s *Store) RecordLabel(
	ctx context.Context,
	group, labeler syntax.DID,
	subject Subject,
	labelURI, val string,
	deleted bool,
) error {
	_, _, err := s.recordEvent(
		ctx, group, subject, EventLabel, labeler,
		map[string]any{"uri": labelURI, "val": val, "deleted": deleted}, "",
	)
	return err
}

// ListSubjects returns the group's queue for status, oldest update first, after
// the cursor (the UpdatedAt and key of the last subject seen).
func (s *Store) ListSubjects(
	ctx context.Context,
	group syntax.DID,
	status SubjectStatus,
	limit int,
	after *SubjectView,
) (_ []SubjectView, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.ListSubjects", trace.WithAttributes(
		telemetry.Org(group),
	))
	defer func() { telemetry.End(span, err) }()
	query := s.db.WithContext(ctx).Where("org_id = ? AND status = ?", group, string(status))
	if after != nil {
		query = query.Where(
			"updated_at > ? OR (updated_at = ? AND subject > ?)",
			after.UpdatedAt, after.UpdatedAt, after.Subject.Key(),
		)
	}
	var rows []moderationSubjectRow
	if err := query.Order("updated_at, subject").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list subjects: %w", err)
	}
	views := make([]SubjectView, len(rows))
	for i, row := range rows {
		views[i] = row.view()
	}
	return views, nil
}

// GetSubjectHistory returns a subject's events, oldest first, after the event
// id cursor. A non-empty cid restricts the history to that record version.
func (s *Store) GetSubjectHistory(
	ctx context.Context,
	group syntax.DID,
	subject Subject,
	limit int,
	afterID string,
) (_ []ModerationEvent, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.GetSubjectHistory", trace.WithAttributes(
		telemetry.Org(group),
	))
	defer func() { telemetry.End(span, err) }()
	query := s.db.WithContext(ctx).Where("org_id = ? AND subject = ?", group, subject.Key())
	if subject.CID != "" {
		query = query.Where("cid = ?", subject.CID)
	}
	if afterID != "" {
		query = query.Where("id > ?", afterID)
	}
	var rows []moderationEventRow
	if err := query.Order("id").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("get subject history: %w", err)
	}
	// An ejected member keeps their history, so only a subject with no events
	// is checked against the group.
	if len(rows) == 0 {
		if err := s.validateSubject(ctx, group, subject); err != nil {
			return nil, err
		}
	}
	events := make([]ModerationEvent, len(rows))
	for i, row := range rows {
		events[i] = row.toEvent()
	}
	return events, nil
}
