package spaces

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Blob bytes are globally deduplicated. Upload ownership and live references
// are independent authorization facts; a known CID is never proof of ownership.
// Legacy records do not establish upload ownership. Their owners must upload
// the bytes again and retry the record to establish these authorization facts.
type blobUpload struct {
	Repo syntax.DID `gorm:"primaryKey"`
	CID  string     `gorm:"primaryKey;column:cid"`
}

type spaceBlobRef struct {
	Space      habitat_syntax.SpaceURI `gorm:"primaryKey"`
	Repo       syntax.DID              `gorm:"primaryKey"`
	Collection syntax.NSID             `gorm:"primaryKey"`
	Rkey       syntax.RecordKey        `gorm:"primaryKey"`
	CID        string                  `gorm:"primaryKey;column:cid"`
}

func (s *store) RegisterBlobUpload(ctx context.Context, repo syntax.DID, c cid.Cid) error {
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).
		Create(&blobUpload{Repo: repo, CID: c.String()}).Error
}

func (s *store) SpaceReferencesBlob(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	c cid.Cid,
) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&spaceBlobRef{}).
		Where("space = ? AND cid = ?", space, c.String()).Count(&count).Error
	return count > 0, err
}

func recordBlobCIDs(record MarshaledRecord) ([]string, error) {
	value, err := atdata.UnmarshalCBOR(record)
	if err != nil {
		return nil, fmt.Errorf("decode blob references: %w", err)
	}
	seen := make(map[string]bool)
	var cids []string
	for _, blob := range atdata.ExtractBlobs(value) {
		c := blob.Ref.String()
		if !seen[c] {
			seen[c] = true
			cids = append(cids, c)
		}
	}
	return cids, nil
}

func requireBlobUploads(tx *gorm.DB, repo syntax.DID, cids []string) error {
	if len(cids) == 0 {
		return nil
	}
	var count int64
	if err := tx.Model(&blobUpload{}).
		Where("repo = ? AND cid IN ?", repo, cids).
		Count(&count).
		Error; err != nil {
		return err
	}
	if count != int64(len(cids)) {
		return ErrBlobNotFound
	}
	return nil
}

func replaceSpaceBlobRefs(
	tx *gorm.DB,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
	cids []string,
) error {
	if err := tx.Where("space = ? AND repo = ? AND collection = ? AND rkey = ?", space, repo, collection, rkey).
		Delete(&spaceBlobRef{}).
		Error; err != nil {
		return err
	}
	if len(cids) == 0 {
		return nil
	}
	refs := make([]spaceBlobRef, len(cids))
	for i, c := range cids {
		refs[i] = spaceBlobRef{Space: space, Repo: repo, Collection: collection, Rkey: rkey, CID: c}
	}
	return tx.Create(&refs).Error
}
