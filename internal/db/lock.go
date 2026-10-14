package db

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"
)

// lockPingInterval is how often a held Postgres lock checks that its
// connection, and so the lock, is still alive.
const lockPingInterval = 10 * time.Second

// LockKey names a [Lock]. Every process using the same key on the same
// database contends for the same lock.
type LockKey int64

// Lock is an exclusive lock shared by every process using one database, for
// work only one of them may do at a time.
//
// On Postgres it is a session-level advisory lock held on its own connection,
// so it is released when its holder releases it, exits, or loses the
// connection. A SQLite database is only ever used by one process, so there it
// only excludes other holders in the same process.
type Lock struct {
	db  *gorm.DB
	key LockKey
	// local is held while the lock is, so one process never takes the lock
	// twice. On SQLite it is the whole lock.
	local sync.Mutex
}

// NewLock returns the lock named key on database.
func NewLock(database *gorm.DB, key LockKey) *Lock {
	return &Lock{db: database, key: key}
}

// TryAcquire takes the lock if it is free and reports whether it did. When it
// did, held is cancelled once the lock is released or lost, or ctx is done,
// and the caller must call release to free it.
func (l *Lock) TryAcquire(
	ctx context.Context,
) (held context.Context, release func(), ok bool, err error) {
	if !l.local.TryLock() {
		return nil, nil, false, nil
	}
	if l.db.Name() != "postgres" {
		held, cancel := context.WithCancel(ctx)
		var once sync.Once
		return held, func() {
			once.Do(func() {
				cancel()
				l.local.Unlock()
			})
		}, true, nil
	}
	held, release, ok, err = l.tryAcquirePostgres(ctx)
	if !ok {
		l.local.Unlock()
	}
	return held, release, ok, err
}

func (l *Lock) tryAcquirePostgres(
	ctx context.Context,
) (context.Context, func(), bool, error) {
	sqlDB, err := l.db.DB()
	if err != nil {
		return nil, nil, false, fmt.Errorf("get database handle: %w", err)
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, nil, false, fmt.Errorf("open lock connection: %w", err)
	}
	var got bool
	if err := conn.QueryRowContext(
		ctx, "SELECT pg_try_advisory_lock($1)", int64(l.key),
	).Scan(&got); err != nil {
		_ = conn.Close()
		return nil, nil, false, fmt.Errorf("try advisory lock: %w", err)
	}
	if !got {
		_ = conn.Close()
		return nil, nil, false, nil
	}
	held, cancel := context.WithCancel(ctx)
	stopped := make(chan struct{})
	go watchConn(held, cancel, conn, stopped)
	var once sync.Once
	release := func() {
		once.Do(func() {
			cancel()
			<-stopped
			// Closing the connection would free the lock too, but the pool may
			// keep the session open, so unlock explicitly first.
			unlockCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
			defer done()
			_, _ = conn.ExecContext(unlockCtx, "SELECT pg_advisory_unlock($1)", int64(l.key))
			_ = conn.Close()
			l.local.Unlock()
		})
	}
	return held, release, true, nil
}

// watchConn cancels held when conn stops answering, since the lock is gone
// with its session. It returns, closing stopped, once held is done.
func watchConn(held context.Context, cancel func(), conn *sql.Conn, stopped chan<- struct{}) {
	defer close(stopped)
	ticker := time.NewTicker(lockPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-held.Done():
			return
		case <-ticker.C:
			if err := conn.PingContext(held); err != nil {
				cancel()
				return
			}
		}
	}
}
