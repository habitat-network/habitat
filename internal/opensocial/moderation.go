package opensocial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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
	// ErrInvalidSubject is returned for a subject that is not an account or a
	// record in one of the group's spaces.
	ErrInvalidSubject = errors.New("subject is not an account or a record in this group")
	// ErrSubjectNotFound is returned when a subject has never been reported.
	ErrSubjectNotFound = errors.New("subject has never been reported")
	// ErrNotAppealable is returned for an appeal of a subject that carries
	// neither a label nor an ejection.
	ErrNotAppealable = errors.New("nothing to appeal on this subject")
	// ErrNoReadAccess is returned when a reporter cannot read the subject's space.
	ErrNoReadAccess = errors.New("the reporter cannot read the subject's space")
)

// SubjectStatus is where a subject stands in the moderation queue.
type SubjectStatus string

const (
	StatusOpen     SubjectStatus = "open"
	StatusResolved SubjectStatus = "resolved"
)

// EventKind says what happened in a ModerationEvent.
type EventKind string

const (
	EventReport EventKind = "report"
	EventAppeal EventKind = "appeal"
	EventStatus EventKind = "status"
	EventTag    EventKind = "tag"
	EventLabel  EventKind = "label"
	EventEject  EventKind = "eject"
)

// Subject is an account (URI is empty) or a record in one of the group's
// spaces (URI set, CID optional).
type Subject struct {
	DID syntax.DID
	URI habitat_syntax.SpaceRecordURI
	CID string
}

// ParseSubject reads a subject: a DID names an account, anything else must be
// a space record URI.
func ParseSubject(raw, cid string) (Subject, error) {
	if strings.HasPrefix(raw, "did:") {
		did, err := syntax.ParseDID(raw)
		if err != nil {
			return Subject{}, fmt.Errorf("%w: %w", ErrInvalidSubject, err)
		}
		return Subject{DID: did}, nil
	}
	uri, err := habitat_syntax.ParseSpaceRecordURI(raw)
	if err != nil {
		return Subject{}, fmt.Errorf("%w: %w", ErrInvalidSubject, err)
	}
	return Subject{URI: uri, CID: cid}, nil
}

// Key identifies the subject in a group's queue: its account DID or record URI.
func (s Subject) Key() string {
	if s.URI != "" {
		return string(s.URI)
	}
	return string(s.DID)
}

// Author is the account the subject is or belongs to.
func (s Subject) Author() syntax.DID {
	if s.URI != "" {
		return s.URI.Repo()
	}
	return s.DID
}

// SubjectView is a subject in the moderation queue.
type SubjectView struct {
	Subject     Subject
	Status      SubjectStatus
	Tags        []string
	ReportCount int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Report is the reporter's input to SubmitReport.
type Report struct {
	ReasonType string
	Reason     string
	Rule       string
}

// ModerationEvent is one entry in a subject's history. Payload is the
// kind-specific JSON object.
type ModerationEvent struct {
	ID        string
	Subject   Subject
	Kind      EventKind
	CreatedAt time.Time
	CreatedBy syntax.DID
	Payload   json.RawMessage
}

// SortOrder orders listings of the moderation queue and history.
type SortOrder string

const (
	SortOldest  SortOrder = "oldest"
	SortNewest  SortOrder = "newest"
	SortUpdated SortOrder = "updated"
)

// moderationSubjectRow is the gorm model backing a group's moderation queue.
// An empty CID stands for the subject as a whole.
type moderationSubjectRow struct {
	OrgID       syntax.DID `gorm:"primaryKey;index:idx_moderation_subject_status,priority:1"`
	Subject     string     `gorm:"primaryKey"`
	CID         string     `gorm:"primaryKey;column:cid"`
	Author      syntax.DID `gorm:"not null"`
	Status      string     `gorm:"not null;index:idx_moderation_subject_status,priority:2"`
	Tags        string     `gorm:"not null"` // JSON-encoded []string
	ReportCount int64      `gorm:"not null"`
	CreatedAt   time.Time  `gorm:"not null"`
	UpdatedAt   time.Time  `gorm:"not null;index:idx_moderation_subject_status,priority:3"`
}

// moderationEventRow is the gorm model backing a subject's history.
type moderationEventRow struct {
	ID        string     `gorm:"primaryKey"`
	OrgID     syntax.DID `gorm:"not null;index:idx_moderation_event_subject,priority:1;index:idx_moderation_event_author,priority:1"`
	Subject   string     `gorm:"not null;index:idx_moderation_event_subject,priority:2"`
	CID       string     `gorm:"not null;column:cid"`
	Author    syntax.DID `gorm:"not null;index:idx_moderation_event_author,priority:2"`
	Kind      string     `gorm:"not null"`
	CreatedBy syntax.DID `gorm:"not null"`
	Payload   string     `gorm:"not null"`
	CreatedAt time.Time  `gorm:"not null"`
}

func subjectOf(key, cid string) Subject {
	if strings.HasPrefix(key, "did:") {
		return Subject{DID: syntax.DID(key)}
	}
	return Subject{URI: habitat_syntax.SpaceRecordURI(key), CID: cid}
}

func (r moderationSubjectRow) view() SubjectView {
	var tags []string
	_ = json.Unmarshal([]byte(r.Tags), &tags)
	return SubjectView{
		Subject: subjectOf(r.Subject, r.CID), Status: SubjectStatus(r.Status), Tags: tags,
		ReportCount: r.ReportCount, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func (r moderationEventRow) toEvent() ModerationEvent {
	return ModerationEvent{
		ID: r.ID, Subject: subjectOf(r.Subject, r.CID), Kind: EventKind(r.Kind),
		CreatedAt: r.CreatedAt, CreatedBy: r.CreatedBy, Payload: json.RawMessage(r.Payload),
	}
}

// validateSubject checks that subject is an account that is, or was, a member,
// or a record in a space the group owns.
func (s *Store) validateSubject(ctx context.Context, group syntax.DID, subject Subject) error {
	if subject.URI != "" {
		if subject.URI.SpaceOwner() != group {
			return ErrInvalidSubject
		}
		return nil
	}
	roles, err := s.GetUserRoles(ctx, group, subject.DID)
	if err != nil {
		return fmt.Errorf("get user roles: %w", err)
	}
	if len(roles) > 0 {
		return nil
	}
	// A former member stays a subject while the queue still holds their history.
	var count int64
	if err := s.db.WithContext(ctx).Model(&moderationSubjectRow{}).
		Where("org_id = ? AND author = ?", group, subject.DID).Limit(1).Count(&count).Error; err != nil {
		return fmt.Errorf("count subjects: %w", err)
	}
	if count == 0 {
		return ErrInvalidSubject
	}
	return nil
}

// recordEvent appends an event to the subject's history and updates its queue
// row, creating the row open when the subject has none. A nil change leaves the
// row as it is.
func (s *Store) recordEvent(
	ctx context.Context,
	group syntax.DID,
	subject Subject,
	kind EventKind,
	actor syntax.DID,
	payload any,
	change func(*moderationSubjectRow),
) (SubjectView, ModerationEvent, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return SubjectView{}, ModerationEvent{}, fmt.Errorf("marshal event: %w", err)
	}
	now := time.Now().UTC()
	event := moderationEventRow{
		ID: syntax.NewTIDNow(0).String(), OrgID: group, Subject: subject.Key(), CID: subject.CID,
		Author: subject.Author(), Kind: string(kind), CreatedBy: actor, Payload: string(raw),
		CreatedAt: now,
	}
	var view SubjectView
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&event).Error; err != nil {
			return fmt.Errorf("create event: %w", err)
		}
		row := moderationSubjectRow{
			OrgID: group, Subject: subject.Key(), CID: subject.CID, Author: subject.Author(),
			Status: string(StatusOpen), Tags: "[]", CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return fmt.Errorf("create subject: %w", err)
		}
		key := "org_id = ? AND subject = ? AND cid = ?"
		if err := tx.Where(key, group, subject.Key(), subject.CID).First(&row).Error; err != nil {
			return fmt.Errorf("get subject: %w", err)
		}
		if change != nil {
			change(&row)
		}
		row.UpdatedAt = now
		if err := tx.Model(&moderationSubjectRow{}).Where(key, group, subject.Key(), subject.CID).
			Updates(map[string]any{
				"status": row.Status, "tags": row.Tags, "report_count": row.ReportCount,
				"updated_at": now,
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

// SubmitReport files a report on subject against group, opening or reopening
// it. The reporter must be able to read a record's space.
func (s *Store) SubmitReport(
	ctx context.Context,
	group, reporter syntax.DID,
	subject Subject,
	report Report,
) (_ ModerationEvent, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.SubmitReport", trace.WithAttributes(
		telemetry.Org(group),
	))
	defer func() { telemetry.End(span, err) }()
	if err := s.validateSubject(ctx, group, subject); err != nil {
		return ModerationEvent{}, err
	}
	if subject.URI != "" {
		ok, err := s.CheckPermission(ctx, reporter, subject.URI.SpaceURI())
		if err != nil {
			return ModerationEvent{}, err
		}
		if !ok {
			return ModerationEvent{}, ErrNoReadAccess
		}
	}
	_, event, err := s.recordEvent(
		ctx, group, subject, EventReport, reporter, report,
		func(row *moderationSubjectRow) {
			row.Status = string(StatusOpen)
			row.ReportCount++
		},
	)
	return event, err
}

// SubjectUpdate is a moderator's change to a subject. Nil and empty fields are
// left as they are.
type SubjectUpdate struct {
	Status     SubjectStatus
	Note       string
	AddTags    []string
	RemoveTags []string
}

// UpdateSubject changes a subject's status or tags, or adds a note, recording
// each as an event. It returns ErrSubjectNotFound for a subject never reported.
func (s *Store) UpdateSubject(
	ctx context.Context,
	group, moderator syntax.DID,
	subject Subject,
	update SubjectUpdate,
) (_ SubjectView, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.UpdateSubject", trace.WithAttributes(
		telemetry.Org(group),
	))
	defer func() { telemetry.End(span, err) }()
	if err := s.validateSubject(ctx, group, subject); err != nil {
		return SubjectView{}, err
	}
	var current moderationSubjectRow
	err = s.db.WithContext(ctx).First(
		&current, "org_id = ? AND subject = ? AND cid = ?", group, subject.Key(), subject.CID,
	).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return SubjectView{}, ErrSubjectNotFound
	}
	if err != nil {
		return SubjectView{}, fmt.Errorf("get subject: %w", err)
	}
	view := current.view()
	if update.Status != "" || update.Note != "" {
		status := update.Status
		if status == "" {
			status = view.Status
		}
		view, _, err = s.recordEvent(
			ctx, group, subject, EventStatus, moderator,
			map[string]string{"status": string(status), "note": update.Note},
			func(row *moderationSubjectRow) { row.Status = string(status) },
		)
		if err != nil {
			return SubjectView{}, err
		}
	}
	if len(update.AddTags) > 0 || len(update.RemoveTags) > 0 {
		view, _, err = s.recordEvent(
			ctx, group, subject, EventTag, moderator,
			map[string]any{"add": update.AddTags, "remove": update.RemoveTags},
			func(row *moderationSubjectRow) {
				var tags []string
				_ = json.Unmarshal([]byte(row.Tags), &tags)
				for _, tag := range update.AddTags {
					if !slices.Contains(tags, tag) {
						tags = append(tags, tag)
					}
				}
				tags = slices.DeleteFunc(tags, func(tag string) bool {
					return slices.Contains(update.RemoveTags, tag)
				})
				raw, _ := json.Marshal(tags)
				row.Tags = string(raw)
			},
		)
		if err != nil {
			return SubjectView{}, err
		}
	}
	return view, nil
}

// RecordEject notes in the member's history that the group ejected them,
// opening them in the queue.
func (s *Store) RecordEject(ctx context.Context, group, moderator, member syntax.DID) error {
	_, _, err := s.recordEvent(
		ctx, group, Subject{DID: member}, EventEject, moderator, map[string]string{}, nil,
	)
	return err
}

// RecordLabel notes in the subject's history that a label record was written or
// deleted.
func (s *Store) RecordLabel(
	ctx context.Context,
	group, labeler syntax.DID,
	subject Subject,
	labelURI, val string,
	deleted bool,
) error {
	_, _, err := s.recordEvent(
		ctx, group, subject, EventLabel, labeler,
		map[string]any{"uri": labelURI, "val": val, "deleted": deleted}, nil,
	)
	return err
}

// SubmitAppeal reopens a subject that carries a label or an ejection, at the
// request of the account it belongs to. Former members may appeal.
func (s *Store) SubmitAppeal(
	ctx context.Context,
	group, appellant syntax.DID,
	subject Subject,
	note string,
) (_ ModerationEvent, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.SubmitAppeal", trace.WithAttributes(
		telemetry.Org(group),
	))
	defer func() { telemetry.End(span, err) }()
	if subject.Author() != appellant {
		return ModerationEvent{}, ErrInvalidSubject
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&moderationEventRow{}).Where(
		"org_id = ? AND subject = ? AND kind IN ?", group, subject.Key(),
		[]string{string(EventLabel), string(EventEject)},
	).Count(&count).Error; err != nil {
		return ModerationEvent{}, fmt.Errorf("count appealable events: %w", err)
	}
	if count == 0 {
		return ModerationEvent{}, ErrNotAppealable
	}
	_, event, err := s.recordEvent(
		ctx, group, subject, EventAppeal, appellant, map[string]string{"note": note},
		func(row *moderationSubjectRow) { row.Status = string(StatusOpen) },
	)
	return event, err
}

// SubjectFilter selects subjects from a group's queue.
type SubjectFilter struct {
	Status SubjectStatus // empty for all
	Tags   []string      // subjects with any of these; empty for all
	Sort   SortOrder
}

// SubjectCursor is the position after the last subject of a page.
type SubjectCursor struct {
	At  time.Time
	Key string
	CID string
}

// ListSubjects returns the group's queue, after cursor.
func (s *Store) ListSubjects(
	ctx context.Context,
	group syntax.DID,
	filter SubjectFilter,
	limit int,
	after *SubjectCursor,
) (_ []SubjectView, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.ListSubjects", trace.WithAttributes(
		telemetry.Org(group),
	))
	defer func() { telemetry.End(span, err) }()
	column, order, cmp := "created_at", "ASC", ">"
	switch filter.Sort {
	case SortNewest:
		order, cmp = "DESC", "<"
	case SortUpdated:
		column, order, cmp = "updated_at", "DESC", "<"
	}
	query := s.db.WithContext(ctx).Where("org_id = ?", group)
	if filter.Status != "" {
		query = query.Where("status = ?", string(filter.Status))
	}
	if after != nil {
		query = query.Where(
			fmt.Sprintf(
				"%[1]s %[2]s ? OR (%[1]s = ? AND (subject || '|' || cid) %[2]s ?)",
				column,
				cmp,
			),
			after.At,
			after.At,
			after.Key+"|"+after.CID,
		)
	}
	query = query.Order(fmt.Sprintf("%s %s, (subject || '|' || cid) %s", column, order, order))
	if len(filter.Tags) == 0 {
		query = query.Limit(limit)
	}
	var rows []moderationSubjectRow
	if err := query.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list subjects: %w", err)
	}
	views := make([]SubjectView, 0, len(rows))
	for _, row := range rows {
		view := row.view()
		// Tags live in a JSON column, so they are matched here rather than in SQL.
		if len(filter.Tags) > 0 &&
			!slices.ContainsFunc(view.Tags, func(tag string) bool {
				return slices.Contains(filter.Tags, tag)
			}) {
			continue
		}
		views = append(views, view)
		if len(views) == limit {
			break
		}
	}
	return views, nil
}

// HistoryFilter selects events from a group's moderation history.
type HistoryFilter struct {
	Subject *Subject    // nil for every subject
	Author  syntax.DID  // only events on this account and its records; empty for all
	Kinds   []EventKind // only these kinds; empty for all
	Sort    SortOrder
}

// GetHistory returns moderation events, newest first by default, after the
// event id cursor. A subject with no events must still be in the group.
func (s *Store) GetHistory(
	ctx context.Context,
	group syntax.DID,
	filter HistoryFilter,
	limit int,
	afterID string,
) (_ []ModerationEvent, err error) {
	ctx, span := tracer.Start(ctx, "opensocial.GetHistory", trace.WithAttributes(
		telemetry.Org(group),
	))
	defer func() { telemetry.End(span, err) }()
	query := s.db.WithContext(ctx).Where("org_id = ?", group)
	if filter.Subject != nil {
		query = query.Where("subject = ?", filter.Subject.Key())
		if filter.Subject.CID != "" {
			query = query.Where("cid = ?", filter.Subject.CID)
		}
	}
	if filter.Author != "" {
		query = query.Where("author = ?", filter.Author)
	}
	if len(filter.Kinds) > 0 {
		kinds := make([]string, len(filter.Kinds))
		for i, kind := range filter.Kinds {
			kinds[i] = string(kind)
		}
		query = query.Where("kind IN ?", kinds)
	}
	order, cmp := "DESC", "<"
	if filter.Sort == SortOldest {
		order, cmp = "ASC", ">"
	}
	if afterID != "" {
		query = query.Where("id "+cmp+" ?", afterID)
	}
	var rows []moderationEventRow
	if err := query.Order("id " + order).Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("get history: %w", err)
	}
	if len(rows) == 0 && afterID == "" && filter.Subject != nil {
		if err := s.validateSubject(ctx, group, *filter.Subject); err != nil {
			return nil, err
		}
	}
	events := make([]ModerationEvent, len(rows))
	for i, row := range rows {
		events[i] = row.toEvent()
	}
	return events, nil
}
