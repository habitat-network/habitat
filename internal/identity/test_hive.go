package identity

import (
	"context"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/hive"
)

// testHive is a hive.Hive that also signs for did:web:*.test identities with
// their derived key (see TestSigningKey), so the passthrough login's
// identities can mint service auth, delegation tokens and space credentials.
type testHive struct {
	hive.Hive
	hostKey atcrypto.PrivateKeyExportable
}

// NewTestHive wraps h so PrivateKeyForDID serves did:web:*.test identities.
func NewTestHive(h hive.Hive, hostKey atcrypto.PrivateKeyExportable) hive.Hive {
	return &testHive{Hive: h, hostKey: hostKey}
}

// PrivateKeyForDID implements hive.Hive.
func (h *testHive) PrivateKeyForDID(
	ctx context.Context,
	did syntax.DID,
) (atcrypto.PrivateKey, error) {
	if IsTestDID(did) {
		return TestSigningKey(h.hostKey, did)
	}
	return h.Hive.PrivateKeyForDID(ctx, did)
}

// WithTx implements hive.Hive, keeping the wrapper around the transaction's hive.
func (h *testHive) WithTx(tx *gorm.DB) hive.Hive {
	return &testHive{Hive: h.Hive.WithTx(tx), hostKey: h.hostKey}
}
