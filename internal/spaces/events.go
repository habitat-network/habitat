package spaces

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/telemetry"
)

// EventSeq is a position in the host's event log: the sequence the
// network.habitat.space.subscribe stream is ordered and resumed by. Seqs
// increase but are not contiguous. 0 is never assigned; a row holding it is
// pending (see [Store.SequenceEvents]).
type EventSeq int64

// Event is one entry of the host's event log: a [CommitEvent] or a
// [SpaceDeletedEvent].
type Event interface {
	isEvent()
	// EventSeq is the event's position in the log.
	EventSeq() EventSeq
	// EventSpace is the space the event belongs to.
	EventSpace() habitat_syntax.SpaceURI
}

// CommitEvent is a record's latest write. The log is compacted: a record has
// one event, at the seq of its latest write, so earlier versions drop out.
type CommitEvent struct {
	Seq        EventSeq
	Space      habitat_syntax.SpaceURI
	Repo       syntax.DID
	Collection syntax.NSID
	Rkey       syntax.RecordKey
	Rev        syntax.TID
	Operation  WriteAction
	// Value and Cid are unset for WriteDelete.
	Value map[string]any
	Cid   cid.Cid
	// Time is when the write was stored.
	Time time.Time
}

// SpaceDeletedEvent is the deletion of a space.
type SpaceDeletedEvent struct {
	Seq   EventSeq
	Space habitat_syntax.SpaceURI
	Time  time.Time
}

func (CommitEvent) isEvent()                                    {}
func (e CommitEvent) EventSeq() EventSeq                        { return e.Seq }
func (e CommitEvent) EventSpace() habitat_syntax.SpaceURI       { return e.Space }
func (SpaceDeletedEvent) isEvent()                              {}
func (e SpaceDeletedEvent) EventSeq() EventSeq                  { return e.Seq }
func (e SpaceDeletedEvent) EventSpace() habitat_syntax.SpaceURI { return e.Space }

// EventFilter narrows [Store.ListEvents]. Each field matches everything when
// empty, and the fields are ANDed. Repos and the collection fields only apply
// to [CommitEvent]s; a [SpaceDeletedEvent] is matched by Spaces alone.
type EventFilter struct {
	Spaces []habitat_syntax.SpaceURI
	Repos  []syntax.DID
	// Collections are exact collection NSIDs, and CollectionPrefixes are
	// prefixes ending in "." (from a "<prefix>.*" pattern); a commit matches
	// if its collection matches either.
	Collections        []syntax.NSID
	CollectionPrefixes []string
}

// spaceEventSeq is the event log's counter: a single row holding the latest
// seq [Store.SequenceEvents] assigned. Sequencing locks it, so sequencers
// (from any pear instance) run one at a time and their seqs commit in order.
type spaceEventSeq struct {
	ID  int `gorm:"primaryKey;autoIncrement:false"`
	Seq EventSeq
}

const (
	// eventSeqRow is the ID of spaceEventSeq's only row.
	eventSeqRow = 1
	// sequenceBatchSize caps how many rows one sequencing transaction numbers,
	// so a large backlog (such as every row from before the log existed) is
	// worked through in bounded transactions.
	sequenceBatchSize = 500
)

// SequenceEvents implements [Store].
func (s *store) SequenceEvents(ctx context.Context) (head EventSeq, err error) {
	ctx, span := tracer.Start(ctx, "spaces.SequenceEvents")
	defer func() { telemetry.End(span, err) }()
	db := s.db.WithContext(ctx)
	sequenced := 0
	for {
		pending, err := hasPendingEvents(db)
		if err != nil {
			return 0, err
		}
		if !pending {
			break
		}
		n, err := sequenceBatch(db)
		if err != nil {
			return 0, err
		}
		sequenced += n
		if n == 0 {
			// Everything pending is locked by in-flight writes, which leave
			// it pending again when they commit.
			break
		}
	}
	span.SetAttributes(attribute.Int("habitat.events.sequenced", sequenced))
	var row spaceEventSeq
	err = db.Where("id = ?", eventSeqRow).Limit(1).Find(&row).Error
	if err != nil {
		return 0, fmt.Errorf("read event log head: %w", err)
	}
	return row.Seq, nil
}

// hasPendingEvents reports whether any committed row is waiting for a seq. It
// takes no locks, so a SequenceEvents with nothing to do never writes.
func hasPendingEvents(db *gorm.DB) (bool, error) {
	var revs []string
	if err := db.Unscoped().Model(&spaceRecord{}).
		Where("seq = 0").Limit(1).Pluck("rev", &revs).Error; err != nil {
		return false, fmt.Errorf("check pending records: %w", err)
	}
	if len(revs) > 0 {
		return true, nil
	}
	if err := db.Unscoped().Model(&spaceRemoteRecord{}).
		Where("seq = 0").Limit(1).Pluck("rev", &revs).Error; err != nil {
		return false, fmt.Errorf("check pending remote records: %w", err)
	}
	if len(revs) > 0 {
		return true, nil
	}
	var owners []string
	if err := db.Model(&deletedSpace{}).
		Where("seq = 0").Limit(1).Pluck("owner", &owners).Error; err != nil {
		return false, fmt.Errorf("check pending space deletions: %w", err)
	}
	return len(owners) > 0, nil
}

// sequenceBatch numbers up to sequenceBatchSize pending rows in one
// transaction, records first in rev order, then records fetched from external
// repos in rev order, then space deletions, and returns how many it numbered.
//
// The counter row's lock, held until commit, is what orders seqs: a seq only
// becomes visible once every lower one has committed. Pending rows are locked
// with SKIP LOCKED, so the sequencer never waits on a writer: a row an
// in-flight write holds is about to be rewritten and left pending again (or,
// if that write rolls back, is still pending), so a later batch numbers it.
func sequenceBatch(db *gorm.DB) (int, error) {
	n := 0
	err := db.Transaction(func(tx *gorm.DB) error {
		var head EventSeq
		// An upsert, so the counter row needn't be seeded, that locks it.
		if err := tx.Raw(
			`INSERT INTO space_event_seqs (id, seq) VALUES (?, 0)
			ON CONFLICT (id) DO UPDATE SET seq = space_event_seqs.seq
			RETURNING seq`,
			eventSeqRow,
		).Scan(&head).Error; err != nil {
			return fmt.Errorf("lock event log: %w", err)
		}
		skipLocked := clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}

		var records []spaceRecord
		if err := tx.Unscoped().Clauses(skipLocked).
			Select("space", "repo", "collection", "rkey", "rev").
			Where("seq = 0").Order("rev ASC").Limit(sequenceBatchSize).
			Find(&records).Error; err != nil {
			return fmt.Errorf("list pending records: %w", err)
		}
		for _, r := range records {
			head++
			if err := tx.Unscoped().Model(&spaceRecord{}).
				Where("space = ? AND repo = ? AND collection = ? AND rkey = ? AND rev = ?",
					r.Space, r.Repo, r.Collection, r.Rkey, r.Rev).
				Update("seq", head).Error; err != nil {
				return fmt.Errorf("sequence record: %w", err)
			}
		}

		// Records fetched from external repos' PDSes, also in rev order.
		var remotes []spaceRemoteRecord
		if room := sequenceBatchSize - len(records); room > 0 {
			if err := tx.Unscoped().Clauses(skipLocked).
				Select("space", "repo", "collection", "rkey", "rev").
				Where("seq = 0").Order("rev ASC").Limit(room).
				Find(&remotes).Error; err != nil {
				return fmt.Errorf("list pending remote records: %w", err)
			}
		}
		for _, r := range remotes {
			head++
			// Matching the rev leaves a row a later fetch has rewritten since,
			// pending again, for the next batch.
			if err := tx.Unscoped().Model(&spaceRemoteRecord{}).
				Where("space = ? AND repo = ? AND collection = ? AND rkey = ? AND rev = ?",
					r.Space, r.Repo, r.Collection, r.Rkey, r.Rev).
				Update("seq", head).Error; err != nil {
				return fmt.Errorf("sequence remote record: %w", err)
			}
		}

		var deletions []deletedSpace
		if room := sequenceBatchSize - len(records) - len(remotes); room > 0 {
			if err := tx.Clauses(skipLocked).
				Where("seq = 0").Order("deleted_at ASC").Limit(room).
				Find(&deletions).Error; err != nil {
				return fmt.Errorf("list pending space deletions: %w", err)
			}
		}
		for _, d := range deletions {
			head++
			if err := tx.Model(&deletedSpace{}).
				Where("owner = ? AND type = ? AND skey = ?", d.Owner, d.Type, d.Skey).
				Update("seq", head).Error; err != nil {
				return fmt.Errorf("sequence space deletion: %w", err)
			}
		}

		n = len(records) + len(remotes) + len(deletions)
		if n == 0 {
			return nil
		}
		return tx.Model(&spaceEventSeq{}).Where("id = ?", eventSeqRow).
			Update("seq", head).Error
	})
	if err != nil {
		return 0, fmt.Errorf("sequence events: %w", err)
	}
	return n, nil
}

// ListEvents implements [Store].
func (s *store) ListEvents(
	ctx context.Context,
	filter EventFilter,
	cursor EventSeq,
	limit int,
) (_ []Event, err error) {
	ctx, span := tracer.Start(ctx, "spaces.ListEvents", trace.WithAttributes(
		attribute.Int64("habitat.events.cursor", int64(cursor)),
		attribute.Int("habitat.limit", limit),
	))
	defer func() { telemetry.End(span, err) }()
	if limit <= 0 {
		limit = 100
	}
	// 0 marks a pending row, which isn't in the log yet.
	cursor = max(cursor, 1)
	db := s.db.WithContext(ctx)

	recordQuery := db.Unscoped().Model(&spaceRecord{}).Where("seq >= ?", cursor)
	if len(filter.Spaces) > 0 {
		recordQuery = recordQuery.Where("space IN ?", filter.Spaces)
	}
	if len(filter.Repos) > 0 {
		recordQuery = recordQuery.Where("repo IN ?", filter.Repos)
	}
	if cond, args := collectionCondition(filter); cond != "" {
		recordQuery = recordQuery.Where(cond, args...)
	}
	var records []spaceRecord
	if err := recordQuery.Order("seq ASC").Limit(limit).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list record events: %w", err)
	}

	// Records external repos hold on their own PDSes, as fetched from there.
	remoteQuery := db.Unscoped().Model(&spaceRemoteRecord{}).Where("seq >= ?", cursor)
	if len(filter.Spaces) > 0 {
		remoteQuery = remoteQuery.Where("space IN ?", filter.Spaces)
	}
	if len(filter.Repos) > 0 {
		remoteQuery = remoteQuery.Where("repo IN ?", filter.Repos)
	}
	if cond, args := collectionCondition(filter); cond != "" {
		remoteQuery = remoteQuery.Where(cond, args...)
	}
	var remotes []spaceRemoteRecord
	if err := remoteQuery.Order("seq ASC").Limit(limit).Find(&remotes).Error; err != nil {
		return nil, fmt.Errorf("list remote record events: %w", err)
	}

	deletionQuery := db.Model(&deletedSpace{}).Where("seq >= ?", cursor)
	if len(filter.Spaces) > 0 {
		cond, args := spaceKeyCondition(filter.Spaces)
		deletionQuery = deletionQuery.Where(cond, args...)
	}
	var deletions []deletedSpace
	if err := deletionQuery.Order("seq ASC").Limit(limit).Find(&deletions).Error; err != nil {
		return nil, fmt.Errorf("list space deletion events: %w", err)
	}

	// Merge the seq-ordered lists, keeping the first limit events. Each list
	// was capped at limit, so the first limit of their union are among them,
	// and fewer than limit merged means every list ran out.
	events := make([]Event, 0, len(records)+len(remotes)+len(deletions))
	for _, r := range records {
		event, err := commitEvent(r)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	for _, r := range remotes {
		event, err := remoteCommitEvent(r)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	for _, d := range deletions {
		events = append(events, SpaceDeletedEvent{
			Seq:   d.Seq,
			Space: habitat_syntax.ConstructSpaceURI(d.Owner, d.Type, d.Skey),
			Time:  d.DeletedAt,
		})
	}
	slices.SortFunc(events, func(a, b Event) int {
		return cmp.Compare(a.EventSeq(), b.EventSeq())
	})
	events = events[:min(limit, len(events))]
	span.SetAttributes(attribute.Int("habitat.events", len(events)))
	return events, nil
}

// collectionCondition is filter's collection constraint as one SQL condition,
// or "" when it has none.
func collectionCondition(filter EventFilter) (string, []any) {
	var conds []string
	var args []any
	if len(filter.Collections) > 0 {
		conds = append(conds, "collection IN ?")
		args = append(args, filter.Collections)
	}
	for _, prefix := range filter.CollectionPrefixes {
		conds = append(conds, `collection LIKE ? ESCAPE '\'`)
		args = append(args, escapeLike(prefix)+"%")
	}
	if len(conds) == 0 {
		return "", nil
	}
	return "(" + strings.Join(conds, " OR ") + ")", args
}

// spaceKeyCondition matches the deletedSpace rows of spaces, which are keyed
// by the parts of the space URI rather than the URI itself.
func spaceKeyCondition(spaces []habitat_syntax.SpaceURI) (string, []any) {
	conds := make([]string, len(spaces))
	args := make([]any, 0, 3*len(spaces))
	for i, space := range spaces {
		conds[i] = "(owner = ? AND type = ? AND skey = ?)"
		args = append(args, space.SpaceOwner(), space.SpaceType(), space.Skey())
	}
	return "(" + strings.Join(conds, " OR ") + ")", args
}

// commitEvent is row's latest write as an event. A row without a prior CID
// was created by that write; a soft-deleted one was deleted by it.
func commitEvent(row spaceRecord) (CommitEvent, error) {
	event := CommitEvent{
		Seq:        row.Seq,
		Space:      row.Space,
		Repo:       row.Repo,
		Collection: row.Collection,
		Rkey:       row.Rkey,
		Rev:        row.Rev,
		Time:       row.UpdatedAt,
	}
	if row.DeletedAt.Valid {
		event.Operation = WriteDelete
		event.Time = row.DeletedAt.Time
		return event, nil
	}
	event.Operation = WriteUpdate
	if row.PrevCid == "" {
		event.Operation = WriteCreate
	}
	value, err := atdata.UnmarshalCBOR(row.Value)
	if err != nil {
		return CommitEvent{}, fmt.Errorf("decode record %s: %w", row.Rev, err)
	}
	c, err := cid.Parse(row.Cid)
	if err != nil {
		return CommitEvent{}, fmt.Errorf("parse record cid %s: %w", row.Rev, err)
	}
	event.Value = value
	event.Cid = c
	return event, nil
}
