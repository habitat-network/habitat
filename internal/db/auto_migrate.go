package db

import "gorm.io/gorm"

type MigratableStore interface {
	Models() []any
}

func AutoMigrate(db *gorm.DB, stores ...any) error {
	models := []any{}
	for _, store := range stores {
		if ms, ok := store.(MigratableStore); ok {
			models = append(models, ms.Models()...)
		}
	}
	return db.AutoMigrate(models...)
}
