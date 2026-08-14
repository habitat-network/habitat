package utils

import (
	"encoding/base64"
	"errors"
	"fmt"
)

// ErrInvalidBytesField is returned by ParseBytes when input is not a valid
// lexicon bytes field.
var ErrInvalidBytesField = errors.New("invalid bytes field")

// ParseBytes decodes a lexicon bytes field, JSON-decoded into a
// map[string]any of the form {"$bytes": "<base64>"} per
// https://atproto.com/specs/lexicon, into raw bytes. A nil input decodes to
// nil.
func ParseBytes(input any) ([]byte, error) {
	if input == nil {
		return nil, nil
	}
	values, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: not a map[string]any", ErrInvalidBytesField)
	}
	s, ok := values["$bytes"].(string)
	if !ok {
		return nil, fmt.Errorf("%w: $bytes is not a string", ErrInvalidBytesField)
	}
	b, err := base64.RawStdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("%w: decode base64: %w", ErrInvalidBytesField, err)
	}
	return b, nil
}
