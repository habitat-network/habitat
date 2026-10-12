package testutil

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	pear_testutil "github.com/habitat-network/habitat/cmd/pear/testutil"
	"github.com/habitat-network/habitat/internal/authn"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	"github.com/habitat-network/habitat/internal/clientmetadata"
	"github.com/habitat-network/habitat/internal/emaildomain"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/forwarding"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/mcpgateway"
	"github.com/habitat-network/habitat/internal/notify"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/internal/pearserver"
	"github.com/habitat-network/habitat/internal/perms"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/searchconfig"
	"github.com/habitat-network/habitat/internal/simplespace"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	"github.com/habitat-network/habitat/internal/utils"
)

// TestServer wraps a constructed *pearserver.PearServer along with the backing
// stores it shares, so tests can seed or inspect data directly.
type TestServer struct {
	Server *pearserver.PearServer

	Validator        authn.RequestValidator
	PermStore        perms.Store
	SpaceStore       spaces.Store
	OpenSocialStore  *opensocial.Store
	SimpleStore      *simplespace.Store
	NotifyStore      notify.Store
	Hive             hive.Hive
	HostKey          atcrypto.PrivateKey
	DB               *gorm.DB
	FGA              fgastore.Store
	McpGatewayStore  mcpgateway.Store
	NangoClient      *FakeNangoClient
	PDSForwarding    *forwarding.PDSForwarding
	EmailDomainStore *emaildomain.Store
	Directory        identity.Directory
	// SearchIndex backs searchRecords; search is off when it is nil.
	SearchIndex search.Index
	// SearchConfig stores the collections each org surfaces in search.
	SearchConfig *searchconfig.Store
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

func WithNotifyStore(store notify.Store) utils.Opt[TestServer] {
	return func(o *TestServer) {
		o.NotifyStore = store
	}
}

func WithNangoClient(client *FakeNangoClient) utils.Opt[TestServer] {
	return func(o *TestServer) {
		o.NangoClient = client
	}
}

func WithFGA(fga fgastore.Store) utils.Opt[TestServer] {
	return func(o *TestServer) {
		o.FGA = fga
	}
}

// WithSearchIndex turns search on, over index.
func WithSearchIndex(index search.Index) utils.Opt[TestServer] {
	return func(o *TestServer) {
		o.SearchIndex = index
	}
}

func WithPDSForwarding(f *forwarding.PDSForwarding) utils.Opt[TestServer] {
	return func(o *TestServer) {
		o.PDSForwarding = f
	}
}

// WithDirectory supplies the identity directory RegisterNotify resolves
// service identifiers through, so a test can publish a DID document naming the
// endpoint its syncer should be delivered to.
func WithDirectory(dir identity.Directory) utils.Opt[TestServer] {
	return func(o *TestServer) {
		o.Directory = dir
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
		ts.DB = pear_testutil.NewPearDB(t)
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
	if ts.NotifyStore == nil {
		notifyStore, err := notify.NewStore(ts.DB)
		require.NoError(t, err)
		ts.NotifyStore = notifyStore
	}
	blobStore := spaces_testutil.NewTestBlobStore(t)

	os, err := opensocial.NewStore(ts.DB, ts.SpaceStore, blobStore, ts.Hive)
	require.NoError(t, err)
	ps := perms.NewStore(ts.DB, ts.SpaceStore, ts.FGA, os)
	ss := simplespace.NewStore(ts.DB, ts.SpaceStore, ps)

	ts.OpenSocialStore = os
	ts.SimpleStore = ss

	if ts.NangoClient == nil {
		ts.NangoClient = NewFakeNangoClient()
	}
	mcpGatewayStore, err := mcpgateway.NewStore(ts.NangoClient, os)
	require.NoError(t, err)
	ts.McpGatewayStore = mcpGatewayStore

	if ts.PDSForwarding == nil {
		// Default forwarding points nowhere; getSession's remote-identity path
		// forwards to a caller's real PDS, which tests exercise by injecting
		// their own forwarding via WithPDSForwarding. The credential store is
		// fully unused on that path.
		ts.PDSForwarding = forwarding.NewPDSForwarding(
			nil,
			ts.Validator,
			pdsclient.NewDummyClientFactory("http://127.0.0.1:1"),
			pdsclient.NewDummyDirectory("http://127.0.0.1:1"),
		)
	}

	if ts.Directory == nil {
		// Empty by default: most tests never register a notify subscriber, and
		// a resolution attempt against it should fail rather than hit the
		// network. Tests that do register one supply their own via
		// WithDirectory.
		ts.Directory = identity.NewMockDirectory()
	}

	emailDomainStore, err := emaildomain.NewStore(ts.DB)
	require.NoError(t, err)
	ts.EmailDomainStore = emailDomainStore

	ts.SearchConfig = searchconfig.NewStore(ts.SpaceStore)
	var searcher *search.Searcher
	if ts.SearchIndex != nil {
		searcher = search.NewSearcher(
			ts.SearchIndex, os, ts.SpaceStore, search.WithCollections(ts.SearchConfig),
		)
	}

	ts.Server = pearserver.New(
		"pear.example.com",
		ts.Validator,
		ts.Directory,
		ts.Hive,
		ts.HostKey,
		blobStore,
		ts.SpaceStore,
		os,
		ps,
		ss,
		ts.NotifyStore,
		clientmetadata.NewResolver(),
		mcpGatewayStore,
		ts.PDSForwarding,
		emailDomainStore,
		searcher,
		ts.SearchConfig,
	)
	ts.PermStore = ps
	return &ts
}

var (
	org   = syntax.DID("did:plc:org")
	owner = syntax.DID("did:plc:owner")
)
