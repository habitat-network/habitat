package db

import (
	"net/url"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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

func TestNewWithGORMConfig(t *testing.T) {
	dir := t.TempDir()
	db, err := New("sqlite://"+dir+"/test.db", WithGORMConfig(&gorm.Config{
		TranslateError: false,
	}))
	require.NoError(t, err)
	require.Equal(t, false, db.TranslateError)
}

func TestNewWithoutMigrations(t *testing.T) {
	dir := t.TempDir()
	db, err := New("sqlite://" + dir + "/test.db")
	require.NoError(t, err)
	require.NotNil(t, db)
	require.Equal(t, true, db.TranslateError)
}

func TestEnsureUTF8ClientEncoding(t *testing.T) {
	tests := []struct {
		name  string
		dsn   string
		query url.Values
	}{
		{
			name:  "no query",
			dsn:   "postgres://user:pass@localhost:5432/pear",
			query: url.Values{"client_encoding": {"UTF8"}},
		},
		{
			name: "existing params and unix socket host are preserved",
			dsn:  "postgresql://pear:p%40ss@/pear?host=/cloudsql/proj:us-west1:db&sslmode=disable",
			query: url.Values{
				"host":            {"/cloudsql/proj:us-west1:db"},
				"sslmode":         {"disable"},
				"client_encoding": {"UTF8"},
			},
		},
		{
			name:  "non-UTF8 client_encoding is overridden",
			dsn:   "postgres://localhost/pear?client_encoding=LATIN1",
			query: url.Values{"client_encoding": {"UTF8"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EnsureUTF8ClientEncoding(tt.dsn)
			require.NoError(t, err)
			u, err := url.Parse(got)
			require.NoError(t, err)
			require.Equal(t, tt.query, u.Query())
			orig, err := url.Parse(tt.dsn)
			require.NoError(t, err)
			require.Equal(t, orig.User.String(), u.User.String())
			require.Equal(t, orig.Host, u.Host)
			require.Equal(t, orig.Path, u.Path)
		})
	}
}

func TestEnsureUTF8ClientEncodingInvalidDSN(t *testing.T) {
	_, err := EnsureUTF8ClientEncoding("postgres://%zz")
	require.Error(t, err)
}

func TestDialect(t *testing.T) {
	require.Equal(t, Postgres, ParseDialect("postgres://user:pass@localhost:5432"))
	require.Equal(t, Sqlite, ParseDialect("sqlite://file.db"))
	require.Empty(t, ParseDialect("foo://bar"))
}
