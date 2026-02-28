package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bradenaw/juniper/xslices"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	_ "github.com/mattn/go-sqlite3"
)

type Repo interface {
	PutRecord(ctx context.Context, record Record, validate *bool) (habitat_syntax.HabitatURI, error)
	GetRecord(ctx context.Context, did string, collection string, rkey string) (*Record, error)
	UploadBlob(ctx context.Context, did string, data []byte, mimeType string) (*BlobRef, error)
	GetBlob(ctx context.Context, did string, cid string) (string /* mimetype */, []byte /* raw blob */, error)
	GetBlobLinks(ctx context.Context, cid syntax.CID) ([]habitat_syntax.HabitatURI, error)
	ListRecords(ctx context.Context, perms []permissions.Permission) ([]Record, error)
}

// Persist private data within repos that mirror public repos.
// A repo currently implements four basic methods: putRecord, getRecord, uploadBlob, getBlob
// In the future, it is possible to implement sync endpoints and other methods.

// A sqlite-backed repo per user contains the following two columns:
// [did, record key, record value]
// For now, store all records in the same database. Eventually, this should be broken up into
// per-user databases or per-user MST repos.

// We really shouldn't have unexported types that get passed around outside the package, like to `main.go`
// Leaving this as-is for now.
type repo struct {
	db *gorm.DB
}

// repo implements the public type Repo
var _ Repo = &repo{}

// Exported type that represents a repo record.
type Record struct {
	Did        string
	Collection string
	Rkey       string
	Value      map[string]any // A JSON blob
}

// Internal type for row representation in the database
type record struct {
	Did        string `gorm:"primaryKey"`
	Collection string `gorm:"primaryKey"`
	Rkey       string `gorm:"primaryKey"`
	Value      []byte
}

type Blob struct {
	gorm.Model
	Did      string
	Cid      string
	MimeType string
	Blob     []byte
}

// Bi-mapping of blob <---> record reference
// Can be used in permissioning blobs, and for garbage collection of blobs that are unreferenced.
type link struct {
	Ref habitat_syntax.HabitatURI `gorm:"primaryKey"`
	Cid syntax.CID                `gorm:"primaryKey"`
	Did syntax.DID                `gorm:"primaryKey"`
}

// TODO: create table etc.
func NewRepo(db *gorm.DB) (*repo, error) {
	if err := db.AutoMigrate(&record{}, &Blob{}, &link{}); err != nil {
		return nil, err
	}
	return &repo{
		db: db,
	}, nil
}

// putRecord puts a record for the given rkey into the repo no matter what; if a record always exists, it is overwritten.
func (r *repo) PutRecord(
	ctx context.Context,
	rec Record,
	validate *bool,
) (habitat_syntax.HabitatURI, error) {
	if validate != nil && *validate {
		err := atdata.Validate(rec.Value)
		if err != nil {
			return "", fmt.Errorf("unable to validate record: %w", err)
		}
	}

	// TODO: find a way to extraact blobs without marshalling + unmarshalling
	marshalled, err := json.Marshal(rec.Value)
	if err != nil {
		return "", err
	}

	// atdata.UnmarshalJSON unmarshals the type with structured atdata types, so that ExtractBlobs works
	// However, atdata.Validate needs to be called on a generic map[string]any (not with the atdata structured types)
	val, err := atdata.UnmarshalJSON(marshalled)
	if err != nil {
		return "", fmt.Errorf("unable to umarshal: %w", err)
	}

	uri := habitat_syntax.ConstructHabitatUri(rec.Did, rec.Collection, rec.Rkey)
	blobs := atdata.ExtractBlobs(val)
	fmt.Println("got blobs", blobs)

	refs := xslices.Map(blobs, func(b atdata.Blob) link {
		return link{
			Ref: uri,
			Cid: syntax.CID(b.Ref.String()),
			Did: syntax.DID(rec.Did),
		}
	})

	// Store rkey directly (no concatenation with collection)
	// Always put (even if something exists).
	err = r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("did = ?", rec.Did).
			Where("collection = ?", rec.Collection).
			Where("rkey = ?", rec.Rkey).
			Delete(&record{}).
			Error; err != nil {
			return err
		}
		bytes, err := json.Marshal(val)
		if err != nil {
			return err
		}
		r := record{Did: rec.Did, Rkey: rec.Rkey, Collection: rec.Collection, Value: bytes}
		if err := tx.Create(&r).Error; err != nil {
			return err
		}
		if len(refs) > 0 {
			if err := tx.Create(&refs).Error; err != nil {
				return err
			}
		}
		return nil
	})

	return uri, err
}

var (
	ErrRecordNotFound       = fmt.Errorf("record not found")
	ErrMultipleRecordsFound = fmt.Errorf("multiple records found for desired query")
)

func (r *repo) GetRecord(
	ctx context.Context,
	did string,
	collection string,
	rkey string,
) (*Record, error) {
	// Query using separate collection and rkey fields
	row, err := gorm.G[record](
		r.db,
	).Where("did = ? AND collection = ? AND rkey = ?", did, collection, rkey).
		First(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrRecordNotFound
	} else if err != nil {
		return nil, err
	}

	var value map[string]any
	err = json.Unmarshal(row.Value, &value)
	if err != nil {
		return nil, err
	}

	return &Record{
		Did:        row.Did,
		Collection: row.Collection,
		Rkey:       row.Rkey,
		Value:      value,
	}, nil
}

type BlobRef struct {
	Ref      atdata.CIDLink `json:"cid"`
	MimeType string         `json:"mimetype"`
	Size     int64          `json:"size"`
}

func (r *repo) UploadBlob(
	ctx context.Context,
	did string,
	data []byte,
	mimeType string,
) (*BlobRef, error) {
	// "blessed" CID type: https://atproto.com/specs/blob#blob-metadata
	cid, err := cid.NewPrefixV1(cid.Raw, multihash.SHA2_256).Sum(data)
	if err != nil {
		return nil, err
	}

	err = gorm.G[Blob](
		r.db,
		clause.OnConflict{UpdateAll: true},
	).Create(ctx, &Blob{
		Did:      did,
		Cid:      cid.String(),
		MimeType: mimeType,
		Blob:     data,
	})
	if err != nil {
		return nil, err
	}

	return &BlobRef{
		Ref:      atdata.CIDLink(cid),
		MimeType: mimeType,
		Size:     int64(len(data)),
	}, nil
}

// getBlob gets a blob. this is never exposed to the server, because blobs can only be resolved via records that link them (see LexLink)
// besides exceptional cases like data migration which we do not support right now.
func (r *repo) GetBlob(
	ctx context.Context,
	did string,
	cid string,
) (string /* mimetype */, []byte /* raw blob */, error) {
	row, err := gorm.G[Blob](
		r.db,
	).Where("did = ? and cid = ?", did, cid).First(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil, ErrRecordNotFound
	} else if err != nil {
		return "", nil, err
	}

	return row.MimeType, row.Blob, nil
}

// GetRefs implements Repo.
func (r *repo) GetBlobLinks(ctx context.Context, cid syntax.CID) ([]habitat_syntax.HabitatURI, error) {
	var links []link
	err := r.db.Where("cid = ?", cid).Find(&links).Error
	if err != nil {
		return nil, err
	}

	return xslices.Map(links, func(l link) habitat_syntax.HabitatURI {
		return l.Ref
	}), nil
}

// listRecords implements repo.
// perms should not include redundant permissions ie
// if grantee has permission to the collection, perms should not include permissions to specific records in that collection
func (r *repo) ListRecords(ctx context.Context, perms []permissions.Permission) ([]Record, error) {
	if len(perms) == 0 {
		return []Record{}, nil
	}

	// Start with base query filtering by did and collection
	query := r.db

	allowQuery := r.db
	for _, perm := range perms {
		if perm.Effect == permissions.Allow {
			grantQuery := r.db.Where("did = ?", perm.Owner)
			if perm.Collection != "" {
				grantQuery = grantQuery.Where("collection = ?", perm.Collection)
			}
			if perm.Rkey != "" {
				// if rkey is not empty
				grantQuery = grantQuery.Where("rkey = ?", perm.Rkey)
			}
			// build up allow `OR`s
			allowQuery = allowQuery.Or(grantQuery)
		} else {
			// build up deny `NOT`s
			query = query.Not(
				r.db.Where("collection = ?", perm.Collection).Where("rkey = ?", perm.Rkey),
			)
		}
	}
	query = query.Where(allowQuery)

	// Order by rkey for consistent pagination
	query = query.Order("rkey ASC")

	// Execute query
	var rows []record
	if err := query.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	records := make([]Record, len(rows))
	for i, row := range rows {

		var value map[string]any
		err := json.Unmarshal(row.Value, &value)
		if err != nil {
			return nil, err
		}

		r := Record{
			Did:        row.Did,
			Collection: row.Collection,
			Rkey:       row.Rkey,
			Value:      value,
		}

		records[i] = r
	}

	return records, nil
}
