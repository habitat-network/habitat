package db

import (
	"fmt"
	"io/fs"
	"net/url"
	"strings"

	"github.com/habitat-network/habitat/internal/utils"
	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/plugin/opentelemetry/tracing"
)

type Dialect string

var (
	Sqlite   Dialect = "sqlite3"
	Postgres Dialect = "postgres"
)

type config struct {
	migrations fs.FS
	gormConfig *gorm.Config
}

func WithMigrations(migrations fs.FS) utils.Opt[config] {
	return func(o *config) {
		o.migrations = migrations
	}
}

func WithGORMConfig(cfg *gorm.Config) utils.Opt[config] {
	return func(o *config) {
		o.gormConfig = cfg
	}
}

func New(dsn string, opts ...utils.Opt[config]) (db *gorm.DB, err error) {
	cfg := utils.ResolveOptions(
		config{
			gormConfig: &gorm.Config{
				TranslateError: true,
			},
		},
		opts,
	)
	switch ParseDialect(dsn) {
	case Postgres:
		dsn, err = EnsureUTF8ClientEncoding(dsn)
		if err != nil {
			return nil, err
		}
		db, err = gorm.Open(postgres.Open(dsn), cfg.gormConfig)
		if err != nil {
			return nil, err
		}
	case Sqlite:
		path := strings.TrimPrefix(dsn, "sqlite://")
		db, err = gorm.Open(
			sqlite.Open(
				path+"?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=10000&_txlock=immediate",
			),
			cfg.gormConfig,
		)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported database: %s", dsn)
	}
	if err := db.Use(tracing.NewPlugin(tracing.WithoutQueryVariables())); err != nil {
		return nil, err
	}
	if cfg.migrations != nil {
		sqlDB, err := db.DB()
		if err != nil {
			return nil, err
		}
		goose.SetBaseFS(cfg.migrations)
		if err := goose.SetDialect(string(ParseDialect(dsn))); err != nil {
			return nil, err
		}
		if err := goose.Up(sqlDB, "migrations"); err != nil {
			return nil, err
		}

	}

	return db, nil
}

// EnsureUTF8ClientEncoding returns the Postgres URL DSN with client_encoding=UTF8
// set, overriding any other value.
//
// Without it the server reports the database's own encoding (e.g. SQL_ASCII or
// LATIN1) as client_encoding, and pgx refuses parameterized simple-protocol
// queries, the mode required behind poolers like PgBouncer. Postgres converts
// between the client and database encodings, so asking for UTF8 is always safe.
func EnsureUTF8ClientEncoding(dsn string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("parse postgres dsn: %w", err)
	}
	q := u.Query()
	q.Set("client_encoding", "UTF8")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func ParseDialect(dsn string) Dialect {
	if strings.HasPrefix(dsn, "postgres") {
		return Postgres
	}
	if strings.HasPrefix(dsn, "sqlite") {
		return Sqlite
	}
	return ""
}
