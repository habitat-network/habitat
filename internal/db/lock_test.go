package db

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/gorm"
)

// testPostgresURLEnv names an environment variable holding the URL of a
// running Postgres for tests to use instead of starting a container.
const testPostgresURLEnv = "HABITAT_TEST_PGURL"

const testLockKey LockKey = 42

var (
	postgresOnce sync.Once
	postgresURL  string
	postgresErr  error
)

// newPostgres returns a fresh connection to the shared test Postgres, so two
// calls act like two processes.
func newPostgres(t *testing.T) *gorm.DB {
	t.Helper()
	url := os.Getenv(testPostgresURLEnv)
	if url == "" {
		postgresOnce.Do(func() {
			ctx := context.Background()
			var c testcontainers.Container
			c, postgresErr = testcontainers.GenericContainer(
				ctx,
				testcontainers.GenericContainerRequest{
					ContainerRequest: testcontainers.ContainerRequest{
						Image:        "postgres:16",
						ExposedPorts: []string{"5432/tcp"},
						Env:          map[string]string{"POSTGRES_HOST_AUTH_METHOD": "trust"},
						WaitingFor:   wait.ForListeningPort("5432/tcp"),
					},
					Started: true,
				},
			)
			if postgresErr != nil {
				return
			}
			var endpoint string
			endpoint, postgresErr = c.PortEndpoint(ctx, "5432/tcp", "")
			postgresURL = "postgres://postgres@" + endpoint + "/postgres?sslmode=disable"
		})
		require.NoError(t, postgresErr)
		url = postgresURL
	}
	d, err := New(url)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := d.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return d
}

func newSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	d, err := New("sqlite://" + filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	return d
}

func requireDone(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("held context is not done")
	}
}

func TestLockExcludesOtherHoldersOnSQLite(t *testing.T) {
	lock := NewLock(newSQLite(t), testLockKey)
	held, release, ok, err := lock.TryAcquire(t.Context())
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, held.Err())

	_, _, ok, err = lock.TryAcquire(t.Context())
	require.NoError(t, err)
	require.False(t, ok, "a held lock was acquired again")

	release()
	release() // releasing twice is harmless
	requireDone(t, held)

	_, release, ok, err = lock.TryAcquire(t.Context())
	require.NoError(t, err)
	require.True(t, ok)
	release()
}

func TestLockExcludesOtherProcessesOnPostgres(t *testing.T) {
	first := NewLock(newPostgres(t), testLockKey)
	second := NewLock(newPostgres(t), testLockKey)

	held, release, ok, err := first.TryAcquire(t.Context())
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, held.Err())

	_, _, ok, err = second.TryAcquire(t.Context())
	require.NoError(t, err)
	require.False(t, ok, "another process acquired a held lock")
	_, _, ok, err = first.TryAcquire(t.Context())
	require.NoError(t, err)
	require.False(t, ok, "the holder acquired its lock twice")

	release()
	requireDone(t, held)

	held, release, ok, err = second.TryAcquire(t.Context())
	require.NoError(t, err)
	require.True(t, ok, "the lock was not freed on release")
	release()
	requireDone(t, held)
}

func TestLockIsFreedWhenItsContextEnds(t *testing.T) {
	lock := NewLock(newPostgres(t), testLockKey+1)
	ctx, cancel := context.WithCancel(t.Context())
	held, release, ok, err := lock.TryAcquire(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	cancel()
	<-held.Done()
	release()

	other := NewLock(newPostgres(t), testLockKey+1)
	_, release, ok, err = other.TryAcquire(t.Context())
	require.NoError(t, err)
	require.True(t, ok)
	release()
}

func TestLockReportsAnUnreachableDatabase(t *testing.T) {
	d := newPostgres(t)
	sqlDB, err := d.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	lock := NewLock(d, testLockKey+2)
	_, _, ok, err := lock.TryAcquire(t.Context())
	require.Error(t, err)
	require.False(t, ok)
	// A failed attempt leaves the lock free in this process.
	_, _, ok, err = lock.TryAcquire(t.Context())
	require.Error(t, err)
	require.False(t, ok)
}
