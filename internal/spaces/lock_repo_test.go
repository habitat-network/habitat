package spaces_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// SQLite serializes whole transactions, so LockRepo is a no-op there; this
// pins that it's callable on a tx-scoped store without error.
func TestStoreLockRepo(t *testing.T) {
	db := db_testutil.NewDB(t)
	s := spaces_testutil.NewTestStore(t, spaces_testutil.WithDB(db))
	owner := syntax.DID("did:plc:owner")
	space := habitat_syntax.ConstructSpaceURI(owner, "community.opensocial.members", "self")
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return s.WithTx(tx).LockRepo(t.Context(), space, owner)
	}))
}
