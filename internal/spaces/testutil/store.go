package testutil

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"gocloud.dev/blob/memblob"

	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/notify/testutil"
	"github.com/habitat-network/habitat/internal/spacecommit"
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
	hostKey  atcrypto.PrivateKey
	signer   spacecommit.MemberSigner
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

func WithHostKey(key atcrypto.PrivateKey) Option {
	return func(o *testOptions) {
		o.hostKey = key
	}
}

func WithMemberSigner(signer spacecommit.MemberSigner) Option {
	return func(o *testOptions) {
		o.signer = signer
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

	key, err := atcrypto.GeneratePrivateKeyK256()
	require.NoError(t, err)

	options := &testOptions{
		fga:      fga,
		db:       db_testutil.NewDB(t),
		hostKey:  key,
		signer:   noMemberSigner{},
		notifier: &testutil.TestNotifier{},
	}

	for _, o := range opts {
		o(options)
	}

	s, err := spaces.NewStore(
		options.db,
		options.notifier,
		spacecommit.NewAuthority(options.hostKey, options.signer),
	)
	require.NoError(t, err)
	return s
}

// noMemberSigner never resolves a habitat-managed signer, so Authority.Build
// always falls back to the host key.
type noMemberSigner struct{}

func (noMemberSigner) PrivateKeyForDID(
	context.Context, syntax.DID,
) (atcrypto.PrivateKey, error) {
	return nil, identity.ErrDIDNotFound
}
