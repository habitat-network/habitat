package testutil

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"gocloud.dev/blob/memblob"

	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/notify/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
)

// NewTestBlobStore returns a spaces.BlobStore backed by an in-memory bucket,
// closed automatically on test cleanup.
func NewTestBlobStore(t *testing.T) spaces.BlobStore {
	t.Helper()
	bucket := memblob.OpenBucket(nil)
	t.Cleanup(func() { _ = bucket.Close() })
	return spaces.NewBlobStore(bucket)
}

type testOptions struct {
	fga      fgastore.Store
	db       *gorm.DB
	notifier spaces.Notifier
}

type Option func(*testOptions)

func WithFGA(fga fgastore.Store) Option {
	return func(o *testOptions) {
		o.fga = fga
	}
}

func WithDB(db *gorm.DB) Option {
	return func(o *testOptions) {
		o.db = db
	}
}

func WithNotifier(notifier spaces.Notifier) Option {
	return func(o *testOptions) {
		o.notifier = notifier
	}
}

func NewTestStore(t *testing.T, opts ...Option) spaces.Store {
	t.Helper()

	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)

	options := &testOptions{
		fga:      fga,
		db:       db_testutil.NewDB(t, spaces.Models()),
		notifier: &testutil.TestNotifier{},
	}

	for _, o := range opts {
		o(options)
	}

	s, err := spaces.NewStore(options.db, options.notifier)
	require.NoError(t, err)
	return s
}

// MustMarshalRecord validates and CBOR-encodes value the way a real
// PutRecord caller must before calling the store's PutRecord.
func MustMarshalRecord(t *testing.T, value any) spaces.MarshaledRecord {
	t.Helper()
	record, err := spaces.MarshalRecord(value)
	require.NoError(t, err)
	return record
}
