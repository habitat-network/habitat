package search_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	pear_testutil "github.com/habitat-network/habitat/cmd/pear/testutil"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/search/searchtest"
)

// TestSQLiteIndex runs the conformance tests over a fresh SQLite database with
// pear's migrations applied, which create the FTS5 index. It needs a build with
// the sqlite_fts5 tag.
func TestSQLiteIndex(t *testing.T) {
	searchtest.Run(t, func(t *testing.T) search.Index {
		idx, err := search.New(pear_testutil.NewPearDB(t))
		require.NoError(t, err)
		return idx
	})
}
