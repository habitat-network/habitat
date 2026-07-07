package db

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var (
	sqliteDialect   = "sqlite3"
	postgresDialect = "postgres"
)

type config struct {
	migrations fs.FS
}

type Option func(*config)

func WithMigrations(migrations fs.FS) Option {
	return func(o *config) {
		o.migrations = migrations
	}
}

func New(dsn string, opts ...Option) (db *gorm.DB, err error) {
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}
	gormConfig := &gorm.Config{
		TranslateError: true,
	}
	switch dialect(dsn) {
	case postgresDialect:
		db, err = gorm.Open(postgres.Open(dsn), gormConfig)
		if err != nil {
			return nil, err
		}
	case sqliteDialect:
		path := strings.TrimPrefix(dsn, "sqlite://")
		db, err = gorm.Open(sqlite.Open(path), gormConfig)
		if err != nil {
			return nil, err
		}
		db.Exec("PRAGMA journal_mode=WAL;")
		db.Exec("PRAGMA synchronous=NORMAL;")
		db.Exec("PRAGMA busy_timeout=10000;")
	default:
		return nil, fmt.Errorf("unsupported database: %s", dsn)
	}

	if cfg.migrations != nil {
		sqlDB, err := db.DB()
		if err != nil {
			return nil, err
		}
		goose.SetBaseFS(cfg.migrations)
		if err := goose.SetDialect(dialect(dsn)); err != nil {
			return nil, err
		}
		if err := goose.Up(sqlDB, "migrations"); err != nil {
			return nil, err
		}

	}

	return db, nil
}

func dialect(dsn string) string {
	if strings.HasPrefix(dsn, "postgres") {
		return postgresDialect
	}
	if strings.HasPrefix(dsn, "sqlite") {
		return sqliteDialect
	}
	return ""
}
