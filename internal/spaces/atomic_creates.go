package spaces

import (
	"context"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"gorm.io/gorm"
)

const MaxCreateBatch = 100

var (
	ErrInvalidBatch        = errors.New("invalid create batch")
	ErrRecordAlreadyExists = errors.New("record already exists")
)

// CreateWrite is an immutable record creation. Updates and physical deletion
// are deliberately absent: mailbox changes can append state and tombstones.
type CreateWrite struct {
	Collection syntax.NSID
	Rkey       syntax.RecordKey
	Value      MarshaledRecord
}

type CreateResult struct {
	URI habitat_syntax.SpaceRecordURI
	CID string
}

// ApplyCreates owns the transaction for one exact Space and member repo.
// Records, live blob references, and the repo hash commit together. Any key
// collision, including a deleted key, rejects the whole batch. Callers recover
// a lost response by reading their operation claim, never by overwriting it.
func (s *store) ApplyCreates(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	writes []CreateWrite,
) ([]CreateResult, error) {
	if s.transactionScoped || len(writes) == 0 || len(writes) > MaxCreateBatch {
		return nil, ErrInvalidBatch
	}
	seen := make(map[string]struct{}, len(writes))
	for _, w := range writes {
		if _, err := syntax.ParseNSID(w.Collection.String()); err != nil {
			return nil, ErrInvalidBatch
		}
		if _, err := syntax.ParseRecordKey(w.Rkey.String()); err != nil {
			return nil, ErrInvalidBatch
		}
		if len(w.Value) == 0 {
			return nil, ErrInvalidBatch
		}
		key := w.Collection.String() + "/" + w.Rkey.String()
		if _, duplicate := seen[key]; duplicate {
			return nil, ErrInvalidBatch
		}
		seen[key] = struct{}{}
	}
	var results []CreateResult
	pending := &batchNotification{}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockRepo(tx, space, repo); err != nil {
			return err
		}
		writer := *s
		writer.db = tx
		writer.notifier = pending
		for _, w := range writes {
			var count int64
			if err := tx.Unscoped().
				Model(&spaceRecord{}).
				Where("space = ? AND repo = ? AND collection = ? AND rkey = ?", space, repo, w.Collection, w.Rkey).
				Count(&count).
				Error; err != nil {
				return err
			}
			if count != 0 {
				return ErrRecordAlreadyExists
			}
			uri, c, err := writer.PutRecord(ctx, space, repo, w.Collection, w.Rkey, w.Value)
			if err != nil {
				return err
			}
			results = append(results, CreateResult{URI: uri, CID: c.String()})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("atomic record creation: %w", err)
	}
	if s.notifier != nil && pending.rev != "" {
		s.notifier.NotifyWrite(ctx, space, repo, pending.rev, pending.hash)
	}
	return results, nil
}

// The ordinary PutRecord path reports each new head. Suppress those reports
// inside the outer transaction and publish its final head only after commit.
type batchNotification struct {
	rev  syntax.TID
	hash []byte
}

func (n *batchNotification) NotifyWrite(
	_ context.Context,
	_ habitat_syntax.SpaceURI,
	_ syntax.DID,
	rev syntax.TID,
	hash []byte,
) {
	n.rev = rev
	n.hash = append(n.hash[:0], hash...)
}
func (*batchNotification) NotifySpaceDeleted(context.Context, habitat_syntax.SpaceURI) {}
