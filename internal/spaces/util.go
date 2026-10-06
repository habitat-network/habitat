package spaces

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// MarshaledRecord is a record value that has been validated against the
// atproto data model and CBOR-encoded by [MarshalRecord]. PutRecord only
// accepts this type so callers can't pass unvalidated bytes by mistake.
type MarshaledRecord []byte

// MarshalRecord validates value against the atproto data model and encodes
// it as the CBOR bytes PutRecord expects. Callers must run their input
// through this (or otherwise produce equivalent, validated CBOR) before
// calling PutRecord.
func MarshalRecord(value any) (MarshaledRecord, error) {
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal value: %w", err)
	}
	// validates against atproto data model
	recordMap, err := atdata.UnmarshalJSON(jsonBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidRecord, err)
	}
	bytes, err := atdata.MarshalCBOR(recordMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal record: %w", err)
	}
	if len(bytes) > atdata.MAX_CBOR_RECORD_SIZE {
		return nil, ErrRecordTooLarge
	}
	return MarshaledRecord(bytes), nil
}

// jsonValue is a record's value as JSON, stored in the database's native JSON
// type (jsonb on Postgres, text holding JSON on SQLite) so records can be
// queried with the database's JSON operators. A nil jsonValue is stored as
// NULL.
type jsonValue []byte

// GormDBDataType picks the column type per dialect.
func (jsonValue) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db.Name() == "postgres" {
		return "jsonb"
	}
	return "text"
}

// Value implements driver.Valuer. It returns a string so drivers send JSON
// text rather than bytes.
func (v jsonValue) Value() (driver.Value, error) {
	if v == nil {
		return nil, nil
	}
	return string(v), nil
}

// Scan implements sql.Scanner.
func (v *jsonValue) Scan(src any) error {
	switch s := src.(type) {
	case nil:
		*v = nil
	case string:
		*v = jsonValue(s)
	case []byte:
		*v = append(jsonValue(nil), s...)
	default:
		return fmt.Errorf("scan jsonValue: unsupported type %T", src)
	}
	return nil
}

// cborToJSON re-encodes a CBOR record value as JSON text for the JSON column.
func cborToJSON(value []byte) (jsonValue, error) {
	record, err := atdata.UnmarshalCBOR(value)
	if err != nil {
		return nil, fmt.Errorf("decode record cbor: %w", err)
	}
	out, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("encode record json: %w", err)
	}
	return jsonValue(out), nil
}
