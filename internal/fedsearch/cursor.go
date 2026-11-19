package fedsearch

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// Cursor points into one stream of a search: the mixed stream, or one
// backend's. Its encoded form is opaque to clients.
type Cursor struct {
	// Search is the search's id.
	Search string `json:"id"`
	// Offset is how many results of the stream the client already has.
	Offset int `json:"n"`
	// Backend selects that backend's stream; empty is the mixed stream.
	Backend BackendID `json:"b,omitempty"`
}

// Encode returns the cursor's opaque form.
func (c Cursor) Encode() string {
	data, _ := json.Marshal(c) // strings and an int can't fail to marshal
	return base64.RawURLEncoding.EncodeToString(data)
}

// DecodeCursor parses a cursor's opaque form. It fails with
// [ErrInvalidCursor].
func DecodeCursor(s string) (Cursor, error) {
	var c Cursor
	data, err := base64.RawURLEncoding.DecodeString(s)
	if err == nil {
		err = json.Unmarshal(data, &c)
	}
	if err != nil {
		return Cursor{}, fmt.Errorf("%w: %w", ErrInvalidCursor, err)
	}
	if c.Search == "" || c.Offset < 0 {
		return Cursor{}, ErrInvalidCursor
	}
	return c, nil
}
