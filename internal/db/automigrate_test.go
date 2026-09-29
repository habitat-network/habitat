package db

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// newTestDB returns an empty SQLite database for the AutoMigrate tests. It is
// a test-local model so that internal/db stays importable from its own tests.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	d, err := New("sqlite://" + filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	return d
}

type testWidget struct {
	ID   uint `gorm:"primaryKey"`
	Name string
}

type testGadget struct {
	ID   uint `gorm:"primaryKey"`
	Name string
}

func hasTable(t *testing.T, db *gorm.DB, name string) bool {
	t.Helper()
	return db.Migrator().HasTable(name)
}

type testModeler struct {
	models []any
}

func (m testModeler) Models() []any { return m.models }

func TestAutoMigrateCreatesEveryModelersTables(t *testing.T) {
	db := newTestDB(t)
	require.False(t, hasTable(t, db, "test_widgets"))
	require.False(t, hasTable(t, db, "test_gadgets"))

	require.NoError(t, AutoMigrate(db,
		testModeler{models: []any{&testWidget{}}},
		testModeler{models: []any{&testGadget{}}},
	))

	require.True(t, hasTable(t, db, "test_widgets"))
	require.True(t, hasTable(t, db, "test_gadgets"))
}

func TestAutoMigrateFlattensMultipleModelsPerModeler(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db, testModeler{
		models: []any{&testWidget{}, &testGadget{}},
	}))
	require.True(t, hasTable(t, db, "test_widgets"))
	require.True(t, hasTable(t, db, "test_gadgets"))
}

func TestAutoMigrateWithoutModelersIsANoop(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))
	require.False(t, hasTable(t, db, "test_widgets"))
}

func TestAutoMigrateIsIdempotent(t *testing.T) {
	db := newTestDB(t)
	modeler := testModeler{models: []any{&testWidget{}}}
	require.NoError(t, AutoMigrate(db, modeler))
	require.NoError(t, AutoMigrate(db, modeler))
	require.True(t, hasTable(t, db, "test_widgets"))
}
