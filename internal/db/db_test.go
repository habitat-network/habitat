package db

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func TestNewRunsMigrations(t *testing.T) {
	dir := t.TempDir()
	db, err := New("sqlite://"+dir+"/test.db", WithMigrations(fstest.MapFS{
		"migrations/20260101000000_create_widgets.sql": &fstest.MapFile{
			Data: []byte(`-- +goose Up
CREATE TABLE widgets (id INTEGER PRIMARY KEY, name TEXT);

-- +goose Down
DROP TABLE widgets;
`),
		},
	}))
	require.NoError(t, err)
	require.NotNil(t, db)

	// The migration ran if the table it defines is queryable.
	require.NoError(t, db.Exec("INSERT INTO widgets (name) VALUES ('a')").Error)

	var count int64
	require.NoError(t, db.Table("widgets").Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestNewWithoutMigrations(t *testing.T) {
	dir := t.TempDir()
	db, err := New("sqlite://" + dir + "/test.db")
	require.NoError(t, err)
	require.NotNil(t, db)
}

func TestDialect(t *testing.T) {
	require.Equal(t, Postgres, ParseDialect("postgres://user:pass@localhost:5432"))
	require.Equal(t, Sqlite, ParseDialect("sqlite://file.db"))
	require.Empty(t, ParseDialect("foo://bar"))
}
