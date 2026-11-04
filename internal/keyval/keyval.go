// Package keyval persists short-lived, JSON-encoded state under (namespace,
// key) pairs with a mandatory TTL, so features that only need to stash a value
// for a few minutes stop needing a table per feature.
//
// Every entry has an explicit lifetime: ttl is a required parameter of Put, and
// callers that really want durable state must say so by passing [NoExpiry].
//
// Expiry is enforced on the read path, so Get, Consume and Delete treat an
// expired entry exactly like one that was never written: both are ErrNotFound.
// Reclaiming the rows is an optimization layered on top of that, not something
// correctness depends on. Postgres reclaims with a trigger installed by pear's
// migrations; SQLite, which has no triggers, sweeps once when [New] builds the
// store.
package keyval

import (
	"context"
	"errors"
	"time"
)

// NoExpiry is the ttl that stores an entry indefinitely. Negative ttls are not
// special: they store an entry that is already expired.
const NoExpiry time.Duration = 0

// ErrNotFound is returned when a key is absent from the store or its entry has
// expired. The two are indistinguishable, because a caller cannot act on them
// differently.
var ErrNotFound = errors.New("keyval: entry not found")

// ErrAlreadyExists is returned by PutIfAbsent when a live entry already holds
// the key.
var ErrAlreadyExists = errors.New("keyval: entry already exists")

// KV is the narrow store surface a consumer that only writes and reads its own
// state depends on.
type KV interface {
	// Put stores value under key, encoded as JSON, and expires it ttl from now,
	// or never if ttl is [NoExpiry]. Writing the same key again replaces both the value and the expiry.
	Put(ctx context.Context, key string, value any, ttl time.Duration) error
	// Get decodes the value stored under key into dst, which must be a non-nil
	// pointer. It returns [ErrNotFound] if the key was never written or has
	// expired.
	Get(ctx context.Context, key string, dst any) error
}

// Store adds the operations that remove state, plus the ability to derive a
// namespaced child.
type Store interface {
	KV
	// PutIfAbsent is Put that refuses to replace a live entry, returning
	// [ErrAlreadyExists] instead. An expired entry counts as absent. The check
	// and the write are one statement, so concurrent callers of the same key
	// yield exactly one success.
	PutIfAbsent(ctx context.Context, key string, value any, ttl time.Duration) error
	// Delete removes the entry under key, returning [ErrNotFound] if there is
	// no live entry for it.
	Delete(ctx context.Context, key string) error
	// Consume reads the value under key into dst and removes the entry in a
	// single statement, so concurrent callers of the same key yield exactly
	// one success and the rest [ErrNotFound].
	Consume(ctx context.Context, key string, dst any) error
	// Namespace returns a store bound to ns. A namespace is part of the primary
	// key, so the returned store can only reach, overwrite or delete its own
	// rows.
	Namespace(ns string) Store
}

// entry is the GORM model for a stored value.
type entry struct {
	Namespace string    `gorm:"column:namespace;primaryKey;size:255"`
	Key       string    `gorm:"column:key;primaryKey;size:255"`
	Value     []byte    `gorm:"column:value;not null"`
	ExpiresAt time.Time `gorm:"column:expires_at;not null;index"`
}

func (entry) TableName() string { return "key_values" }

// Models returns the GORM models this package persists, for db.Migrate.
func Models() []any { return []any{&entry{}} }
