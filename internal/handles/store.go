package handles

import (
	"context"
	"errors"

	"github.com/bluesky-social/indigo/atproto/identity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type handleMapping struct {
	Handle string `gorm:"primaryKey"`
	DID    string
}

type store struct {
	db *gorm.DB
}

func newStore(db *gorm.DB) (*store, error) {
	if err := db.AutoMigrate(&handleMapping{}); err != nil {
		return nil, err
	}
	return &store{db: db}, nil
}

func (s *store) create(ctx context.Context, handle, did string) error {
	result := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&handleMapping{
			Handle: handle,
			DID:    did,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrHandleExists
	}
	return nil
}

func (s *store) get(ctx context.Context, handle string) (string, error) {
	var m handleMapping
	if err := s.db.WithContext(ctx).Where("handle = ?", handle).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", identity.ErrHandleNotFound
		}
		return "", err
	}
	return m.DID, nil
}
