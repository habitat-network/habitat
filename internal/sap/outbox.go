package sap

import (
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/events"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
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

func writeOplogRecords(
	tx *gorm.DB,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	records []habitat.NetworkHabitatSpaceGetRepoOplogRecord,
) error {
	for _, rec := range records {
		collection, err := syntax.ParseNSID(rec.Collection)
		if err != nil {
			return fmt.Errorf("parse collection %q: %w", rec.Collection, err)
		}
		rkey, err := syntax.ParseRecordKey(rec.Rkey)
		if err != nil {
			return fmt.Errorf("parse rkey %q: %w", rec.Rkey, err)
		}
		value, err := json.Marshal(rec.Value)
		if err != nil {
			return err
		}
		uri := habitat_syntax.ConstructSpaceRecordURI(space, repo, collection, rkey)
		if err := tx.Create(&outbox{
			URI:   uri,
			Value: value,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}
