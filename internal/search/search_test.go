package search_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/db"
	dbtestutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/search/searchtest"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const spaceA = habitat_syntax.SpaceURI("at://did:plc:org/space/network.habitat.test/a")

// newSQLiteIndex returns an Index over a fresh SQLite database with the search
// schema applied. It needs a build with the sqlite_fts5 tag.
func newSQLiteIndex(t *testing.T) (search.Index, *gorm.DB) {
	t.Helper()
	gdb := dbtestutil.NewDB(t)
	stmts, err := search.FTSSchema(db.Sqlite)
	require.NoError(t, err)
	for _, stmt := range stmts {
		require.NoError(t, gdb.Exec(stmt).Error)
	}
	idx, err := search.New(gdb)
	require.NoError(t, err)
	return idx, gdb
}

func TestSQLiteIndex(t *testing.T) {
	searchtest.Run(t, func(t *testing.T) search.Index {
		idx, _ := newSQLiteIndex(t)
		return idx
	})
}

func TestSQLiteIndexBackfillsExistingRows(t *testing.T) {
	gdb := dbtestutil.NewDB(t)
	require.NoError(t, gdb.Exec(
		`INSERT INTO search_documents (uri, space, repo, collection, rev, body)
		 VALUES ('at://x', ?, 'did:plc:user', 'network.habitat.note', '3kaaaaaaaaaa2',
		         'preexisting text')`,
		spaceA.String(),
	).Error)
	stmts, err := search.FTSSchema(db.Sqlite)
	require.NoError(t, err)
	for _, stmt := range stmts {
		require.NoError(t, gdb.Exec(stmt).Error)
	}
	idx, err := search.New(gdb)
	require.NoError(t, err)

	res, err := idx.Search(t.Context(), search.Query{
		Text: "preexisting", Spaces: []habitat_syntax.SpaceURI{spaceA},
	})
	require.NoError(t, err)
	require.Len(t, res.Hits, 1)
}

func TestFTSSchemaIsIdempotent(t *testing.T) {
	_, gdb := newSQLiteIndex(t)
	stmts, err := search.FTSSchema(db.Sqlite)
	require.NoError(t, err)
	for _, stmt := range stmts {
		require.NoError(t, gdb.Exec(stmt).Error)
	}
}

func TestFTSSchemaRejectsUnknownDialect(t *testing.T) {
	_, err := search.FTSSchema("mysql")
	require.Error(t, err)
}
