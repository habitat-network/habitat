package spaces

import (
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"gorm.io/datatypes"
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

// cborToJSON re-encodes a CBOR record value as JSON text for the JSON column.
func cborToJSON(value []byte) (datatypes.JSON, error) {
	record, err := atdata.UnmarshalCBOR(value)
	if err != nil {
		return nil, fmt.Errorf("decode record cbor: %w", err)
	}
	out, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("encode record json: %w", err)
	}
	return datatypes.JSON(out), nil
}

// decode returns the record's value, read from the JSON column. Rows with no
// JSON (written by a binary that predates the column, after the backfill ran)
// fall back to the CBOR value.
func (r spaceRecord) decode() (map[string]any, error) {
	if r.noJSON() {
		return atdata.UnmarshalCBOR(r.Value)
	}
	return atdata.UnmarshalJSON(r.ValueJSON)
}

// cbor returns the record's DAG-CBOR block bytes. When they are rebuilt from
// the JSON column the result must hash to the record's stored CID, since the
// CID commits to those exact bytes.
func (r spaceRecord) cbor() ([]byte, error) {
	if r.noJSON() {
		return r.Value, nil
	}
	record, err := atdata.UnmarshalJSON(r.ValueJSON)
	if err != nil {
		return nil, fmt.Errorf("decode record json: %w", err)
	}
	raw, err := atdata.MarshalCBOR(record)
	if err != nil {
		return nil, fmt.Errorf("encode record cbor: %w", err)
	}
	got, err := cid.NewPrefixV1(cid.DagCBOR, multihash.SHA2_256).Sum(raw)
	if err != nil {
		return nil, err
	}
	if got.String() != r.Cid {
		return nil, fmt.Errorf("record json re-encodes to cid %s, want %s", got, r.Cid)
	}
	return raw, nil
}

// noJSON reports whether the row has no JSON value. datatypes.JSON scans a
// NULL column as the JSON text "null"; a record is always an object, so that
// can only mean NULL.
func (r spaceRecord) noJSON() bool {
	return len(r.ValueJSON) == 0 || string(r.ValueJSON) == "null"
}
