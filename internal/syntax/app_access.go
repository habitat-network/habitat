package syntax

import (
	"encoding/base64"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// AppAccessRkey deterministically derives the network.habitat.space.appAccess
// record key for a client_id: its base64url (no padding) encoding, so the
// grant for a given client is directly addressable without a lookup index.
func AppAccessRkey(clientID string) (syntax.RecordKey, error) {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(clientID))
	rkey, err := syntax.ParseRecordKey(encoded)
	if err != nil {
		return "", fmt.Errorf("client id too long to address as a record key: %w", err)
	}
	return rkey, nil
}
