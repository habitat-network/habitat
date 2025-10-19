package privi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	_ "github.com/mattn/go-sqlite3"

	"github.com/bluesky-social/indigo/atproto/data"
)

// Persist private data within repos that mirror public repos.
// A repo implements two methods: putRecord and getRecord.
// In the future, it is possible to implement sync endpoints and other methods.

type repo interface {
	putRecord(did string, rkey string, rec record, validate *bool) error
	getRecord(did string, rkey string) (record, error)
}

// A sqlite-backed repo per user contains the following two columns:
// [did, record key, record value]
// For now, store all records in the same database. Eventually, this should be broken up into
// per-user databases or per-user MST repos.

type sqliteRepo struct {
	db *sql.DB
}

var _ repo = &sqliteRepo{}

// Shape of a row in the sqlite table
type row struct {
	did  string
	rkey string
	rec  string
}

// TODO: create table etc.
func NewSQLiteRepo(db *sql.DB) (repo, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS records (
		did TEXT NOT NULL,
		rkey TEXT NOT NULL,
		record BLOB,
		PRIMARY KEY(did, rkey)
	);`)
	if err != nil {
		return nil, err
	}
	return &sqliteRepo{
		db: db,
	}, nil
}

// putRecord puts a record for the given rkey into the repo no matter what; if a record always exists, it is overwritten.
func (r *sqliteRepo) putRecord(did string, rkey string, rec record, validate *bool) error {
	if validate != nil && *validate {
		err := data.Validate(rec)
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

	var row row
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
