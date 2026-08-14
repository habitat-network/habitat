package testutil

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/notify/testutil"
	"github.com/habitat-network/habitat/internal/spacecommit"
	"github.com/habitat-network/habitat/internal/spaces"
)

type Config struct {
	FgaStore fgastore.Store
	DB       *gorm.DB
	// HostKey signs repo-head commits for authors MemberSigner does not
	// resolve. Generated fresh when unset.
	HostKey atcrypto.PrivateKey
	// MemberSigner resolves habitat-managed authors' own signing keys.
	// Defaults to one that resolves none, since callers' fixture DIDs are
	// never actually hive-managed identities — every commit falls back to
	// HostKey.
	MemberSigner spacecommit.MemberSigner
}

type TestStore struct {
	spaces.Store
	Notifier *testutil.TestNotifier
	FGA      fgastore.Store
	HostKey  atcrypto.PrivateKey
}

func NewTestStore(t *testing.T, cfgs ...Config) *TestStore {
	t.Helper()
	cfg := Config{}
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	if cfg.FgaStore == nil {
		fga, err := fgastore.NewMemory(t.Context())
		require.NoError(t, err)
		t.Cleanup(func() { _ = fga.Close() })
		cfg.FgaStore = fga
	}
	if cfg.DB == nil {
		cfg.DB = db_testutil.NewDB(t)
	}
	if cfg.HostKey == nil {
		key, err := atcrypto.GeneratePrivateKeyK256()
		require.NoError(t, err)
		cfg.HostKey = key
	}
	if cfg.MemberSigner == nil {
		cfg.MemberSigner = noMemberSigner{}
	}
	notifier := &testutil.TestNotifier{}
	s, err := spaces.NewStore(
		cfg.DB,
		cfg.FgaStore,
		notifier,
		spacecommit.NewAuthority(cfg.HostKey, cfg.MemberSigner),
	)
	require.NoError(t, err)
	return &TestStore{
		Store:    s,
		Notifier: notifier,
		FGA:      cfg.FgaStore,
		HostKey:  cfg.HostKey,
	}
}

// FGAStore exposes the underlying FGA store for tests that rebuild a Server
// against an existing test store.
func (t *TestStore) FGAStore() fgastore.Store { return t.FGA }

// noMemberSigner never resolves a habitat-managed signer, so Authority.Build
// always falls back to the host key.
type noMemberSigner struct{}

func (noMemberSigner) PrivateKeyForDID(
	context.Context, syntax.DID,
) (atcrypto.PrivateKey, error) {
	return nil, identity.ErrDIDNotFound
}
