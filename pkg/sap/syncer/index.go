package syncer

import (
	"context"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// repoRecord is the CID sap last saw at one path within a repo. The set of
// these for a repo mirrors what sap has folded into that repo's LtHash and
// emitted downstream.
//
// It exists so recovery can diff the host's path listing against what sap
// already holds and re-emit only the records that actually differ, rather than
// replaying the whole repo through the outbox. Values are not stored: they go
// to the outbox and are the consumer's to keep.
type repoRecord struct {
	Space      habitat_syntax.SpaceURI `gorm:"primaryKey"`
	DID        syntax.DID              `gorm:"column:did;primaryKey"`
	Collection syntax.NSID             `gorm:"primaryKey"`
	Rkey       syntax.RecordKey        `gorm:"primaryKey"`
	Cid        string
}

func (repoRecord) TableName() string { return "sap_repo_records" }

// indexRecord records the CID now at a path, replacing any previous entry.
func indexRecord(
	ctx context.Context,
	tx *gorm.DB,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
	cid string,
) error {
	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "space"}, {Name: "did"}, {Name: "collection"}, {Name: "rkey"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"cid"}),
	}).Create(&repoRecord{
		Space:      space,
		DID:        did,
		Collection: collection,
		Rkey:       rkey,
		Cid:        cid,
	}).Error
}

// forgetRecord drops a path from the index, for an operation that deleted it.
func forgetRecord(
	ctx context.Context,
	tx *gorm.DB,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
) error {
	return tx.WithContext(ctx).
		Where("space = ? AND did = ? AND collection = ? AND rkey = ?",
			space, did, collection, rkey).
		Delete(&repoRecord{}).Error
}

// recordIndex returns the CID sap holds for every path in a repo, keyed by
// "{collection}/{rkey}". An empty result means sap holds nothing for the repo
// and has no basis to diff against.
func recordIndex(
	ctx context.Context,
	db *gorm.DB,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
) (map[string]string, error) {
	var rows []repoRecord
	if err := db.WithContext(ctx).
		Where("space = ? AND did = ?", space, did).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	index := make(map[string]string, len(rows))
	for _, row := range rows {
		index[recordPath(row.Collection, row.Rkey)] = row.Cid
	}
	return index, nil
}

// replaceRecordIndex swaps a repo's whole index for the given paths, for a
// recovery that rebuilt the repo from the host's full state.
func replaceRecordIndex(
	ctx context.Context,
	tx *gorm.DB,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
	rows []repoRecord,
) error {
	if err := tx.WithContext(ctx).
		Where("space = ? AND did = ?", space, did).
		Delete(&repoRecord{}).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.WithContext(ctx).CreateInBatches(rows, 500).Error
}

// recordPath is the index key for a record within a repo.
func recordPath(collection syntax.NSID, rkey syntax.RecordKey) string {
	return collection.String() + "/" + rkey.String()
}
