package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/cmd/pear/migrations"
	"github.com/habitat-network/habitat/internal/db"
)

var (
	keyvalPGOnce sync.Once
	keyvalPGURL  string
	keyvalPGErr  error
)

// newKeyvalPostgres returns a fresh database, on a Postgres container shared by
// these tests, with pear's real migrations applied. Using the committed
// migrations is what makes these tests cover the actual trigger SQL rather than
// a copy of it. They live in cmd/pear because it is the only place that can
// run them without an import cycle: cmd/pear/migrations imports oauthserver,
// which imports keyval.
func newKeyvalPostgres(t *testing.T) *gorm.DB {
	t.Helper()
	keyvalPGOnce.Do(func() {
		ctx := context.Background()
		c, err := postgres.Run(ctx,
			"postgres:16-alpine",
			postgres.WithDatabase("pear"),
			postgres.WithUsername("pear"),
			postgres.WithPassword("pear"),
			tc.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
			),
		)
		if err != nil {
			keyvalPGErr = err
			return
		}
		keyvalPGURL, keyvalPGErr = c.ConnectionString(ctx, "sslmode=disable")
	})
	require.NoError(t, keyvalPGErr)

	admin, err := db.New(keyvalPGURL)
	require.NoError(t, err)
	suffix := make([]byte, 8)
	_, err = rand.Read(suffix)
	require.NoError(t, err)
	name := "trigger_" + hex.EncodeToString(suffix)
	require.NoError(t, admin.Exec(`CREATE DATABASE "`+name+`"`).Error)

	u, err := url.Parse(keyvalPGURL)
	require.NoError(t, err)
	u.Path = "/" + name
	d, err := db.New(u.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := d.DB(); err == nil {
			_ = sqlDB.Close()
		}
		_ = admin.Exec(`DROP DATABASE IF EXISTS "` + name + `" WITH (FORCE)`).Error
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	require.NoError(t, migrations.Run(t.Context(), migrations.PearMigrationContext{DB: d}))
	return d
}

// seedKeyval inserts a row with the sweep trigger disabled, since the trigger
// would otherwise delete an already-expired row the moment it was inserted.
func seedKeyval(t *testing.T, d *gorm.DB, key string, expiresAt time.Time) {
	t.Helper()
	require.NoError(t, d.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(
			`ALTER TABLE key_values DISABLE TRIGGER keyval_sweep_expired_trg`,
		).Error; err != nil {
			return err
		}
		if err := tx.Exec(
			`INSERT INTO key_values (namespace, key, value, expires_at) VALUES ('seed', ?, '{}', ?)`,
			key,
			expiresAt,
		).Error; err != nil {
			return err
		}
		return tx.Exec(`ALTER TABLE key_values ENABLE TRIGGER keyval_sweep_expired_trg`).Error
	}))
}

func keyvalKeys(t *testing.T, d *gorm.DB) []string {
	t.Helper()
	var keys []string
	require.NoError(t, d.Table("key_values").Order("key").Pluck("key", &keys).Error)
	return keys
}

func writeKeyval(d *gorm.DB, key string) error {
	return d.Exec(
		`INSERT INTO key_values (namespace, key, value, expires_at) VALUES ('writer', ?, '{}', ?)`,
		key, time.Now().Add(time.Hour),
	).Error
}

func TestKeyvalTriggerSweepsExpiredOnWrite(t *testing.T) {
	d := newKeyvalPostgres(t)
	seedKeyval(t, d, "expired", time.Now().Add(-time.Hour))
	seedKeyval(t, d, "live", time.Now().Add(time.Hour))
	require.Equal(t, []string{"expired", "live"}, keyvalKeys(t, d))

	require.NoError(t, writeKeyval(d, "third"))

	require.Equal(t, []string{"live", "third"}, keyvalKeys(t, d))
}

func TestKeyvalTriggerLeavesUnexpiredAlone(t *testing.T) {
	d := newKeyvalPostgres(t)
	seedKeyval(t, d, "live", time.Now().Add(time.Hour))

	require.NoError(t, writeKeyval(d, "second"))

	require.Equal(t, []string{"live", "second"}, keyvalKeys(t, d))
}

// The trigger fires AFTER INSERT OR UPDATE only, so its own DELETE does not
// fire it again.
func TestKeyvalTriggerDoesNotRecurse(t *testing.T) {
	d := newKeyvalPostgres(t)
	seedKeyval(t, d, "expired", time.Now().Add(-time.Hour))

	require.NoError(t, writeKeyval(d, "writer"))

	require.Equal(t, []string{"writer"}, keyvalKeys(t, d))
}
