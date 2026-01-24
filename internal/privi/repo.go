package privi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/habitat-network/habitat/api/habitat"
	_ "github.com/mattn/go-sqlite3"
)

// Persist private data within repos that mirror public repos.
// A repo currently implements four basic methods: putRecord, getRecord, uploadBlob, getBlob
// In the future, it is possible to implement sync endpoints and other methods.

// A sqlite-backed repo per user contains the following two columns:
// [did, record key, record value]
// For now, store all records in the same database. Eventually, this should be broken up into
// per-user databases or per-user MST repos.

// We really shouldn't have unexported types that get passed around outside the package, like to `main.go`
// Leaving this as-is for now.
type sqliteRepo struct {
	db *gorm.DB
}

type Record struct {
	Did  string `gorm:"primaryKey"`
	Rkey string `gorm:"primaryKey"`
	Rec  string
}

type Blob struct {
	gorm.Model
	Did      string
	Cid      string
	MimeType string
	Blob     []byte
}

// TODO: create table etc.
func NewSQLiteRepo(db *gorm.DB) (*sqliteRepo, error) {
	if err := db.AutoMigrate(&Record{}, &Blob{}); err != nil {
		return nil, err
	}
	return &sqliteRepo{
		db: db,
	}, nil
}

// hasRepoForDid checks if this instance manges the data for a given did
func (r *sqliteRepo) hasRepoForDid(did string) (bool, error) {
	// TODO: for now, we determine if a did is managed by this instance, by just checking for the existence of any record
	// with the matching DID. In the future, we need to track a formal table of managed repos. There will need to be
	// onboarding and offboard flows as well.
	_, err := gorm.G[Record](r.db).Where("did = ?", did).First(context.Background())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

// putRecord puts a record for the given rkey into the repo no matter what; if a record always exists, it is overwritten.
func (r *sqliteRepo) putRecord(did string, rkey string, rec map[string]any, validate *bool) error {
	if validate != nil && *validate {
		err := atdata.Validate(rec)
		if err != nil {
			return err
		}
	}

	bytes, err := json.Marshal(rec)
	if err != nil {
		return err
	}

	record := Record{Did: did, Rkey: rkey, Rec: string(bytes)}
	// Always put (even if something exists).
	return gorm.G[Record](
		r.db,
		clause.OnConflict{UpdateAll: true},
	).Create(context.Background(), &record)
}

var (
	ErrRecordNotFound       = fmt.Errorf("record not found")
	ErrMultipleRecordsFound = fmt.Errorf("multiple records found for desired query")
)

func (r *sqliteRepo) getRecord(did string, rkey string) (*Record, error) {
	row, err := gorm.G[Record](
		r.db,
	).Where("did = ? and rkey = ?", did, rkey).
		First(context.Background())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrRecordNotFound
	} else if err != nil {
		return nil, err
	}
	return &row, nil
}

type blob struct {
	Ref      atdata.CIDLink `json:"cid"`
	MimeType string         `json:"mimetype"`
	Size     int64          `json:"size"`
}

func (r *sqliteRepo) uploadBlob(did string, data []byte, mimeType string) (*blob, error) {
	// "blessed" CID type: https://atproto.com/specs/blob#blob-metadata
	cid, err := cid.NewPrefixV1(cid.Raw, multihash.SHA2_256).Sum(data)
	if err != nil {
		return nil, err
	}

	err = gorm.G[Blob](
		r.db,
		clause.OnConflict{UpdateAll: true},
	).Create(context.Background(), &Blob{
		Did:      did,
		Cid:      cid.String(),
		MimeType: mimeType,
		Blob:     data,
	})
	if err != nil {
		return nil, err
	}

	return &blob{
		Ref:      atdata.CIDLink(cid),
		MimeType: mimeType,
		Size:     int64(len(data)),
	}, nil
}

// getBlob gets a blob. this is never exposed to the server, because blobs can only be resolved via records that link them (see LexLink)
// besides exceptional cases like data migration which we do not support right now.
func (r *sqliteRepo) getBlob(
	did string,
	cid string,
) (string /* mimetype */, []byte /* raw blob */, error) {
	row, err := gorm.G[Blob](
		r.db,
	).Where("did = ? and cid = ?", did, cid).First(context.Background())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil, ErrRecordNotFound
	} else if err != nil {
		return "", nil, err
	}

	return row.MimeType, row.Blob, nil
}

// listRecords implements repo.
func (r *sqliteRepo) listRecords(
	params *habitat.NetworkHabitatRepoListRecordsParams,
	allow []string,
	deny []string,
) ([]Record, error) {
	if len(allow) == 0 {
		return []Record{}, nil
	}

	query := gorm.G[Record](
		r.db.Debug(),
	).Where("did = ?", params.Repo).
		Where("rkey LIKE ?", params.Collection+".%")

	// Build OR conditions for allow list
	if len(allow) > 0 {
		allowConditions := r.db.Where("1 = 0") // Start with false condition
		for _, a := range allow {
			if strings.HasSuffix(a, "*") {
				// Wildcard match
				prefix := strings.TrimSuffix(a, "*")
				allowConditions = allowConditions.Or("rkey LIKE ?", prefix+"%")
			} else {
				// Exact match
				allowConditions = allowConditions.Or("rkey = ?", a)
			}
		}
		query = query.Where(allowConditions)
	}

	// Build deny conditions - use NOT LIKE or != for each deny pattern
	for _, d := range deny {
		if strings.HasSuffix(d, "*") {
			prefix := strings.TrimSuffix(d, "*")
			query = query.Where("rkey NOT LIKE ?", prefix+"%")
		} else {
			query = query.Where("rkey != ?", d)
		}
	}

	// Cursor-based pagination
	if params.Cursor != "" {
		query = query.Where("rkey > ?", params.Cursor)
	}

	// Limit
	if params.Limit != 0 {
		query = query.Limit(int(params.Limit))
	}

	// Order by rkey for consistent pagination
	query = query.Order("rkey ASC")

	// Execute query
	rows, err := query.Find(context.Background())
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	return rows, nil
}
