package privi

import (
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
	db          *sql.DB
	maxBlobSize int
}

// Helper function to query sqlite compile-time options to get the max blob size
// Not sure if this can change across versions, if so we need to keep that stable
func getMaxBlobSize(db *sql.DB) (int, error) {
	rows, err := db.Query("PRAGMA compile_options;")
	defer util.Close(rows, func(err error) {
		log.Err(err).Msgf("error closing db rows")
	})

	if err != nil {
		return 0, nil
	}

	for rows.Next() {
		var opt string
		_ = rows.Scan(&opt)
		if strings.HasPrefix(opt, "MAX_LENGTH=") {
			return strconv.Atoi(strings.TrimPrefix(opt, "MAX_LENGTH="))
		}
	}
	return 0, fmt.Errorf("no MAX_LENGTH parameter found")
}

// TODO: create table etc.
func NewSQLiteRepo(db *sql.DB) (*sqliteRepo, error) {
	// Create records table
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS records (
		did TEXT NOT NULL,
		rkey TEXT NOT NULL,
		record BLOB,
		PRIMARY KEY(did, rkey)
	);`)
	if err != nil {
		return nil, err
	}

	// Create blobs table
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS blobs (
		did TEXT NOT NULL,
		cid TEXT NOT NULL,
		mimetype TEXT,
		blob BLOB,
		PRIMARY KEY(did, cid)
	);`)
	if err != nil {
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
	// Always put (even if something exists).
	_, err = r.db.Exec(
		"insert or replace into records(did, rkey, record) values(?, ?, jsonb(?));",
		did,
		rkey,
		bytes,
	)
	return err
}

var (
	ErrRecordNotFound       = fmt.Errorf("record not found")
	ErrMultipleRecordsFound = fmt.Errorf("multiple records found for desired query")
)

func (r *sqliteRepo) getRecord(did string, rkey string) (record, error) {
	queried := r.db.QueryRow(
		"select did, rkey, json(record) from records where rkey = ? and did = ?",
		rkey,
		did,
	)

	var row struct {
		did  string
		rkey string
		rec  string
	}
	err := queried.Scan(&row.did, &row.rkey, &row.rec)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRecordNotFound
	} else if err != nil {
		return nil, err
	}

	var record record
	err = json.Unmarshal([]byte(row.rec), &record)
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
		return nil, fmt.Errorf("blob size is too big, must be < max blob size (based on SQLITE MAX_LENGTH compile option): %d bytes", r.maxBlobSize)
	}

	// "blessed" CID type: https://atproto.com/specs/blob#blob-metadata
	cid, err := cid.NewPrefixV1(cid.Raw, multihash.SHA2_256).Sum(data)
	if err != nil {
		return nil, err
	}

	sql := `insert into blobs(did, cid, mimetype, blob) values(?, ?, ?, ?)`
	_, err = r.db.Exec(sql, did, cid.String(), mimeType, data)
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
func (r *sqliteRepo) getBlob(did string, cid string) (string /* mimetype */, []byte /* raw blob */, error) {
	qry := "select mimetype, blob from blobs where did = ? and cid = ?"
	res := r.db.QueryRow(
		qry,
		did,
		cid,
	)

	var mimetype string
	var blob []byte
	err := res.Scan(&mimetype, &blob)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, ErrRecordNotFound
	} else if err != nil {
		return "", nil, err
	}

	return mimetype, blob, nil
}
