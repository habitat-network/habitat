package privi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/eagraf/habitat-new/util"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	db          *gorm.DB
	maxBlobSize int
}

// Helper function to query sqlite compile-time options to get the max blob size
// Not sure if this can change across versions, if so we need to keep that stable
func getMaxBlobSize(db *gorm.DB) (int, error) {
	sqlDb, err := db.DB()
	if err != nil {
		return 0, err
	}

	rows, err := sqlDb.Query("PRAGMA compile_options;")
	if err != nil {
		return 0, err
	}
	defer util.Close(rows, func(err error) {
		log.Err(err).Msgf("error closing db rows")
	})

	for rows.Next() {
		var opt string
		_ = rows.Scan(&opt)
		if strings.HasPrefix(opt, "MAX_LENGTH=") {
			return strconv.Atoi(strings.TrimPrefix(opt, "MAX_LENGTH="))
		}
	}
	return 0, fmt.Errorf("no MAX_LENGTH parameter found")
}

type Record struct {
	gorm.Model
	Did  string
	Rkey string
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

	maxBlobSize, err := getMaxBlobSize(db)
	if err != nil {
		return nil, err
	}

	return &sqliteRepo{
		db:          db,
		maxBlobSize: maxBlobSize,
	}, nil
}

// putRecord puts a record for the given rkey into the repo no matter what; if a record always exists, it is overwritten.
func (r *sqliteRepo) putRecord(did string, rkey string, rec record, validate *bool) error {
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
		r.db.Clauses(clause.OnConflict{UpdateAll: true}),
	).Create(context.Background(), &record)
}

var (
	ErrRecordNotFound       = fmt.Errorf("record not found")
	ErrMultipleRecordsFound = fmt.Errorf("multiple records found for desired query")
)

func (r *sqliteRepo) getRecord(did string, rkey string) (record, error) {
	row, err := gorm.G[Record](
		r.db,
	).Where("did = ? and rkey = ?", did, rkey).
		First(context.Background())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRecordNotFound
	} else if err != nil {
		return nil, err
	}

	var record record
	err = json.Unmarshal([]byte(row.Rec), &record)
	if err != nil {
		return nil, err
	}

	return record, nil
}

type blob struct {
	Ref      atdata.CIDLink `json:"cid"`
	MimeType string         `json:"mimetype"`
	Size     int64          `json:"size"`
}

func (r *sqliteRepo) uploadBlob(did string, data []byte, mimeType string) (*blob, error) {
	// Validate blob size
	if len(data) > r.maxBlobSize {
		return nil, fmt.Errorf(
			"blob size is too big, must be < max blob size (based on SQLITE MAX_LENGTH compile option): %d bytes",
			r.maxBlobSize,
		)
	}

	// "blessed" CID type: https://atproto.com/specs/blob#blob-metadata
	cid, err := cid.NewPrefixV1(cid.Raw, multihash.SHA2_256).Sum(data)
	if err != nil {
		return nil, err
	}

	err = gorm.G[Blob](
		r.db.Clauses(clause.OnConflict{UpdateAll: true}),
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
