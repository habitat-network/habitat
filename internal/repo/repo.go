package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	_ "github.com/mattn/go-sqlite3"
)

type Repo interface {
	PutRecord(ctx context.Context, did string, collection string, rkey string, rec map[string]any, validate *bool) (habitat_syntax.HabitatURI, error)
	GetRecord(ctx context.Context, did string, collection string, rkey string) (*Record, error)
	UploadBlob(ctx context.Context, did string, data []byte, mimeType string) (*BlobRef, error)
	GetBlob(ctx context.Context, did string, cid string) (string /* mimetype */, []byte /* raw blob */, error)
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

type Record struct {
	Did        string `gorm:"primaryKey"`
	Collection string `gorm:"primaryKey"`
	Rkey       string `gorm:"primaryKey"`
	Value      string
}

type Blob struct {
	gorm.Model
	Did      string
	Cid      string
	MimeType string
	Blob     []byte
}

// TODO: create table etc.
func NewRepo(db *gorm.DB) (*repo, error) {
	if err := db.AutoMigrate(&Record{}, &Blob{}); err != nil {
		return nil, err
	}
	return &repo{
		db: db,
	}, nil
}

// putRecord puts a record for the given rkey into the repo no matter what; if a record always exists, it is overwritten.
func (r *repo) PutRecord(ctx context.Context, did string, collection string, rkey string, rec map[string]any, validate *bool) (habitat_syntax.HabitatURI, error) {
	if validate != nil && *validate {
		err := atdata.Validate(rec)
		if err != nil {
			return "", err
		}
	}

	bytes, err := json.Marshal(rec)
	if err != nil {
		return "", err
	}

	// Store rkey directly (no concatenation with collection)
	record := Record{Did: did, Rkey: rkey, Collection: collection, Value: string(bytes)}
	// Always put (even if something exists).
	err = gorm.G[Record](
		r.db,
		clause.OnConflict{
			Columns: []clause.Column{
				{Name: "did"},
				{Name: "collection"},
				{Name: "rkey"},
			},
			DoUpdates: clause.AssignmentColumns([]string{"value"}),
		},
	).Create(ctx, &record)
	if err != nil {
		return "", err
	}

	return habitat_syntax.ConstructHabitatUri(did, collection, rkey), nil
}

var (
	ErrRecordNotFound       = fmt.Errorf("record not found")
	ErrMultipleRecordsFound = fmt.Errorf("multiple records found for desired query")
)

func (r *repo) GetRecord(ctx context.Context, did string, collection string, rkey string) (*Record, error) {
	// Query using separate collection and rkey fields
	row, err := gorm.G[Record](
		r.db,
	).Where("did = ? AND collection = ? AND rkey = ?", did, collection, rkey).
		First(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrRecordNotFound
	} else if err != nil {
		return nil, err
	}
	return &row, nil
}

type BlobRef struct {
	Ref      atdata.CIDLink `json:"cid"`
	MimeType string         `json:"mimetype"`
	Size     int64          `json:"size"`
}

func (r *repo) UploadBlob(ctx context.Context, did string, data []byte, mimeType string) (*BlobRef, error) {
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

// listRecords implements repo.
func (r *repo) ListRecords(ctx context.Context, perms []permissions.Permission) ([]Record, error) {
	if len(perms) == 0 {
		return []Record{}, nil
	}

	// Start with base query filtering by did and collection
	query := r.db

	allowQuery := r.db
	for _, perm := range perms {
		if perm.Effect == "allow" {
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
			query = query.Not(r.db.Where("collection = ?", perm.Collection).Where("rkey = ?", perm.Rkey))
		}
	}
	query = query.Where(allowQuery)

	// Order by rkey for consistent pagination
	query = query.Order("rkey ASC")

	// Execute query
	var rows []Record
	if err := query.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	return rows, nil
}

// listRecordsByOwners queries records from multiple owners for a given collection
func (r *repo) ListRecordsByOwnersDeprecated(ownerDIDs []string, collection string) ([]Record, error) {
	if len(ownerDIDs) == 0 {
		return []Record{}, nil
	}

	query := gorm.G[Record](r.db).
		Where("did IN ?", ownerDIDs).
		Where("collection = ?", collection).
		Order("did ASC, rkey ASC")

	records, err := query.Find(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to query records by owners: %w", err)
	}
	return records, nil
}

// listSpecificRecords queries specific records by a list of (did, rkey) pairs for a given collection
// recordPairs is a slice of structs with Owner and Rkey fields
func (r *repo) ListSpecificRecordsDeprecated(collection string, recordPairs []struct{ Owner, Rkey string }) ([]Record, error) {
	if len(recordPairs) == 0 {
		return []Record{}, nil
	}

	// Build OR conditions for (did, rkey) pairs
	// Format: (did = owner1 AND rkey = rkey1) OR (did = owner2 AND rkey = rkey2) OR ...
	query := gorm.G[Record](r.db).
		Where("collection = ?", collection)

	// Build OR conditions using GORM's Or method
	if len(recordPairs) > 0 {
		query = query.Where("(did = ? AND rkey = ?)", recordPairs[0].Owner, recordPairs[0].Rkey)
		for i := 1; i < len(recordPairs); i++ {
			query = query.Or("(did = ? AND rkey = ?)", recordPairs[i].Owner, recordPairs[i].Rkey)
		}
	}

	query = query.Order("did ASC, rkey ASC")

	records, err := query.Find(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to query specific records: %w", err)
	}
	return records, nil
}
