package sap

import (
	"encoding/json"

	"github.com/habitat-network/habitat/internal/events"
	"gorm.io/gorm"
)

func writeEventOps(tx *gorm.DB, ops []events.EventOps) error {
	for _, op := range ops {
		value, err := json.Marshal(op.Value)
		if err != nil {
			return err
		}
		if err := tx.Create(&outbox{
			URI:   op.Uri,
			Value: value,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}
