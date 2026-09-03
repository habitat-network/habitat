package testutil

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/authn"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	"github.com/habitat-network/habitat/internal/clientmeta"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/pearserver"
	"github.com/habitat-network/habitat/internal/perms"
	"github.com/habitat-network/habitat/internal/simplespace"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	"github.com/habitat-network/habitat/internal/utils"
)

// TestServer wraps a constructed *pearserver.PearServer along with the backing
// stores it shares, so tests can seed or inspect data directly.
type TestServer struct {
	Server *pearserver.PearServer

	Validator       authn.RequestValidator
	PermStore       perms.Store
	SpaceStore      spaces.Store
	OpenSocialStore *opensocial.Store
	SimpleStore     *simplespace.Store
	Hive            hive.Hive
	HostKey         atcrypto.PrivateKey
	DB              *gorm.DB
	FGA             fgastore.Store
}

func WithValidator(validator authn.RequestValidator) utils.Opt[TestServer] {
	return func(o *TestServer) {
		o.Validator = validator
	}
}

func WithHostKey(key atcrypto.PrivateKey) utils.Opt[TestServer] {
	return func(o *TestServer) {
		o.HostKey = key
	}
}

func WithHive(have hive.Hive) utils.Opt[TestServer] {
	return func(o *TestServer) {
		o.Hive = have
	}
}

func WithDB(db *gorm.DB) utils.Opt[TestServer] {
	return func(o *TestServer) {
		o.DB = db
	}
}

func WithSpaceStore(store spaces.Store) utils.Opt[TestServer] {
	return func(o *TestServer) {
		o.SpaceStore = store
	}
}

func WithFGA(fga fgastore.Store) utils.Opt[TestServer] {
	return func(o *TestServer) {
		o.FGA = fga
	}
}

// NewTestServer returns a PearServer wired with throwaway storage, along with
// the stores backing it so tests can seed or inspect records directly. Only
// options are applied if provided; dependencies are otherwise created fresh.
// A generated host key is used by default so delegation-token and
// credential-signing handlers can be exercised.
func NewTestServer(t *testing.T, opts ...utils.Opt[TestServer]) *TestServer {
	t.Helper()

	ts := utils.ResolveOptions(TestServer{}, opts)
	if ts.Validator == nil {
		ts.Validator = authntest.NewSuccessValidatorWithOrg(owner, org)
	}
	if ts.HostKey == nil {
		key, err := atcrypto.GeneratePrivateKeyK256()
		require.NoError(t, err)
		ts.HostKey = key
	}
	if ts.FGA == nil {
		fga, err := fgastore.NewMemory(t.Context())
		require.NoError(t, err)
		t.Cleanup(func() { _ = fga.Close() })
		ts.FGA = fga
	}
	if ts.DB == nil {
		ts.DB = db_testutil.NewDB(t)
	}
	if ts.Hive == nil {
		hiveRep, err := hive.NewHive("example.com", "pear.example.com", ts.DB)
		require.NoError(t, err)
		ts.Hive = hiveRep
	}
	if ts.SpaceStore == nil {
		ts.SpaceStore = spaces_testutil.NewTestStore(
			t,
			spaces_testutil.WithDB(ts.DB),
			spaces_testutil.WithFGA(ts.FGA),
			spaces_testutil.WithHostKey(ts.HostKey),
		)
	}
	blobStore := spaces_testutil.NewTestBlobStore(t)

	os, err := opensocial.NewStore(ts.DB, ts.SpaceStore, blobStore, ts.Hive)
	require.NoError(t, err)
	ps := perms.NewStore(ts.DB, ts.SpaceStore, ts.FGA, os)
	ss := simplespace.NewStore(ts.DB, ts.SpaceStore, ps)

	ts.OpenSocialStore = os
	ts.SimpleStore = ss

	ts.Server = pearserver.New(
		"pear.example.com",
		ts.Validator,
		ts.Hive,
		ts.HostKey,
		blobStore,
		ts.SpaceStore,
		os,
		ps,
		ss,
		clientmeta.NewResolver(),
	)
	ts.PermStore = ps
	return &ts
}

var (
	org   = syntax.DID("did:plc:org")
	owner = syntax.DID("did:plc:owner")
)
