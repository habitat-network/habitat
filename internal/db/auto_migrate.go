package db

import (
	"context"

	"gorm.io/gorm"
)

type MigratableStore interface {
	Models() []any
}

func AutoMigrate(ctx context.Context, db *gorm.DB, stores ...MigratableStore) error {
	models := []any{}
	for _, store := range stores {
		if store == nil {
			continue
		}
		models = append(models, store.Models()...)
	}
	return db.WithContext(ctx).AutoMigrate(models...)
}
