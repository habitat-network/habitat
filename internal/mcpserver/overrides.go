package mcpserver

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// TextOverridesHeader, when the server is built WithTextOverrides, carries
// TextOverrides as base64-encoded JSON, replacing the text agents see for that
// MCP session. integration/mcp-evals uses it to compare wordings without
// rebuilding pear.
const TextOverridesHeader = "X-Habitat-Mcp-Text-Overrides"

// TextOverrides replaces the text MCP clients are sent. A nil field keeps the
// built-in text; an empty one sends none.
type TextOverrides struct {
	// Instructions replaces serverInstructions.
	Instructions *string `json:"instructions,omitempty"`
	// SearchDescription replaces searchRecordsDescription.
	SearchDescription *string `json:"search_description,omitempty"`
}

// EncodeHeader encodes o as a TextOverridesHeader value.
func (o TextOverrides) EncodeHeader() (string, error) {
	data, err := json.Marshal(o)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// parseTextOverrides decodes a TextOverridesHeader value. An empty value is
// no overrides.
func parseTextOverrides(header string) (TextOverrides, error) {
	var o TextOverrides
	if header == "" {
		return o, nil
	}
	data, err := base64.StdEncoding.DecodeString(header)
	if err != nil {
		return o, fmt.Errorf("decoding %s: %w", TextOverridesHeader, err)
	}
	// Reject unknown fields, so a misspelt override fails loudly instead of
	// quietly measuring the built-in text.
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&o); err != nil {
		return o, fmt.Errorf("parsing %s: %w", TextOverridesHeader, err)
	}
	return o, nil
}

// textOr returns *override, or builtIn when override is nil.
func textOr(override *string, builtIn string) string {
	if override != nil {
		return *override
	}
	return builtIn
}

// Option configures New.
type Option func(*options)

type options struct {
	textOverrides bool
}

// WithTextOverrides lets requests override the instructions and the search
// tool's description with TextOverridesHeader. Only for development: it lets
// a caller reword what their own agent is told, nothing more.
func WithTextOverrides() Option {
	return func(o *options) { o.textOverrides = true }
}
