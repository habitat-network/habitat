package keyval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/habitat-network/habitat/internal/db"
)

// errNilDst is returned when Get or Consume is handed no destination, which
// would otherwise panic inside json.Unmarshal.
var errNilDst = errors.New("keyval: dst must be a non-nil pointer")

type store struct {
	db        *gorm.DB
	namespace string
}

var _ Store = (*store)(nil)

// New returns the root store, bound to the empty namespace. Consumers are
// handed a child from Namespace, never the root.
//
// The key_values table must already exist: pear's migrations create it, and
// tests create it from [Models].
//
// On SQLite, which has no triggers, New also sweeps expired rows once. Reads
// filter on expiry regardless, so the sweep only reclaims space.
func New(ctx context.Context, d *gorm.DB) (Store, error) {
	if db.DialectOf(d) == db.Sqlite {
		if err := d.WithContext(ctx).
			Where("expires_at <= ?", time.Now()).
			Delete(&entry{}).Error; err != nil {
			return nil, fmt.Errorf("sweep expired keyval entries: %w", err)
		}
	}
	return &store{db: d}, nil
}

func (s *store) Namespace(ns string) Store {
	return &store{db: s.db, namespace: ns}
}

func (s *store) Put(ctx context.Context, key string, value any, ttl time.Duration) error {
	// Marshal before any database work so a bad value never leaves a partial write.
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal keyval value: %w", err)
	}
	err = s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "namespace"}, {Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "expires_at"}),
	}).Create(&entry{
		Namespace: s.namespace,
		Key:       key,
		Value:     raw,
		ExpiresAt: time.Now().Add(ttl),
	}).Error
	if err != nil {
		return fmt.Errorf("put keyval entry: %w", err)
	}
	return nil
}

func (s *store) Get(ctx context.Context, key string, dst any) error {
	if dst == nil {
		return errNilDst
	}
	var e entry
	err := s.db.WithContext(ctx).
		Select("value").
		Where("namespace = ? AND key = ? AND expires_at > ?", s.namespace, key, time.Now()).
		Take(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("get keyval entry: %w", err)
	}
	if err := json.Unmarshal(e.Value, dst); err != nil {
		return fmt.Errorf("unmarshal keyval value: %w", err)
	}
	return nil
}

func (s *store) Delete(ctx context.Context, key string) error {
	res := s.db.WithContext(ctx).
		Where("namespace = ? AND key = ? AND expires_at > ?", s.namespace, key, time.Now()).
		Delete(&entry{})
	if res.Error != nil {
		return fmt.Errorf("delete keyval entry: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Consume deletes and returns the value in one DELETE ... RETURNING statement,
// which both dialects execute atomically, so concurrent callers cannot both win.
func (s *store) Consume(ctx context.Context, key string, dst any) error {
	if dst == nil {
		return errNilDst
	}
	var entries []entry
	res := s.db.WithContext(ctx).
		Clauses(clause.Returning{Columns: []clause.Column{{Name: "value"}}}).
		Where("namespace = ? AND key = ? AND expires_at > ?", s.namespace, key, time.Now()).
		Delete(&entries)
	if res.Error != nil {
		return fmt.Errorf("consume keyval entry: %w", res.Error)
	}
	if res.RowsAffected == 0 || len(entries) == 0 {
		return ErrNotFound
	}
	if err := json.Unmarshal(entries[0].Value, dst); err != nil {
		return fmt.Errorf("unmarshal keyval value: %w", err)
	}
	return nil
}
