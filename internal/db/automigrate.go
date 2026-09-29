package db

import (
	"fmt"

	"gorm.io/gorm"
)

// Modeler exposes the GORM models a store package persists, so that
// [AutoMigrate] can create their tables. Every store package exports a
// [Modeler] value named Models.
type Modeler interface {
	Models() []any
}

// AutoMigrate creates or updates the tables for every model returned by the
// given modelers, using GORM's schema inference.
//
// It is additive: tables that already exist are left in place, and columns,
// indexes and constraints missing from them are added.
func AutoMigrate(db *gorm.DB, modelers ...Modeler) error {
	if len(modelers) == 0 {
		return nil
	}
	models := make([]any, 0, len(modelers))
	for _, modeler := range modelers {
		models = append(models, modeler.Models()...)
	}
	if err := db.AutoMigrate(models...); err != nil {
		return fmt.Errorf("automigrate: %w", err)
	}
	return nil
}
