package spaces

import (
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/atdata"
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
