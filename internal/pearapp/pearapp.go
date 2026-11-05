// Package pearapp holds pear's application: every store and service pear builds
// from its configuration. cmd/pear constructs one App and passes it to whatever
// runs (the server, migrations, tasks), and handlers such as pearserver take it
// as their only dependency instead of a long list of stores.
package pearapp

import (
	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"gocloud.dev/blob"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/clientmetadata"
	"github.com/habitat-network/habitat/internal/emaildomain"
	"github.com/habitat-network/habitat/internal/forwarding"
	"github.com/habitat-network/habitat/internal/hive"
	habitat_identity "github.com/habitat-network/habitat/internal/identity"
	"github.com/habitat-network/habitat/internal/keyval"
	"github.com/habitat-network/habitat/internal/llmgateway"
	"github.com/habitat-network/habitat/internal/mcpgateway"
	"github.com/habitat-network/habitat/internal/nango"
	"github.com/habitat-network/habitat/internal/notify"
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/pdsoauth"
	"github.com/habitat-network/habitat/internal/perms"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/searchconfig"
	"github.com/habitat-network/habitat/internal/simplespace"
	"github.com/habitat-network/habitat/internal/spaces"
)

// App holds pear's stores and services.
type App struct {
	DB               *gorm.DB
	Domain           string
	HiveDomain       string
	MCPOrigin        string
	OAuthClient      *pdsoauth.App
	Hive             hive.Hive
	DefaultDir       identity.Directory
	HiveDir          identity.Directory
	PDSClientFactory pdsoauth.HttpClientFactory
	EmailDomainStore *emaildomain.Store
	HostKey          atcrypto.PrivateKey
	// HostKEMKey is the host's #atproto_kem key: space commit MACs for repo
	// owners on external PDSes are keyed against it (habitat-managed owners
	// use their own hive KEM key instead). See spacecommit.KEMKeys.
	HostKEMKey      *atcrypto.PrivateKeyP256
	NotifyStore     notify.Store
	Notifier        *notify.Deliverer
	BlobBucket      *blob.Bucket
	BlobStore       spaces.BlobStore
	SpacesStore     spaces.Store
	OpensocialStore *opensocial.Store
	EmailResolver   *habitat_identity.EmailResolver
	OAuthServer     *oauthserver.OAuthServer
	ServiceAuth     *authn.AtprotoServiceAuthMethod
	PermStore       perms.Store
	Validator       authn.RequestValidator
	// CredentialRevocations holds the space credentials authorities revoked
	// through notifyCredentialRevoked; the validator refuses them.
	CredentialRevocations *authn.CredentialRevocations
	// DelegationTokensSpent records used delegation token jtis, so each is
	// accepted by getSpaceCredential only once.
	DelegationTokensSpent keyval.Store
	SimpleStore           *simplespace.Store
	NangoClient           *nango.Client
	MCPGatewayStore       mcpgateway.Store
	LLMStore              *llmgateway.Store
	PDSForwarding         *forwarding.PDSForwarding
	ClientMetadata        *clientmetadata.Resolver
	// SearchIndex is nil when search (--meilisearch_url) is off.
	SearchIndex search.Index
	// SearchIndexer is nil when search (--meilisearch_url) is off.
	SearchIndexer *search.Indexer
	// Searcher is nil when search (--meilisearch_url) is off.
	Searcher     *search.Searcher
	SearchConfig *searchconfig.Store
}

// New returns an App holding the given stores and services.
func New(
	database *gorm.DB,
	domain string,
	hiveDomain string,
	mcpOrigin string,
	oauthClient *pdsoauth.App,
	hive hive.Hive,
	defaultDir identity.Directory,
	hiveDir identity.Directory,
	pdsClientFactory pdsoauth.HttpClientFactory,
	emailDomainStore *emaildomain.Store,
	hostKey atcrypto.PrivateKey,
	hostKEMKey *atcrypto.PrivateKeyP256,
	notifyStore notify.Store,
	notifier *notify.Deliverer,
	blobBucket *blob.Bucket,
	blobStore spaces.BlobStore,
	spacesStore spaces.Store,
	opensocialStore *opensocial.Store,
	emailResolver *habitat_identity.EmailResolver,
	oauthServer *oauthserver.OAuthServer,
	serviceAuth *authn.AtprotoServiceAuthMethod,
	permStore perms.Store,
	validator authn.RequestValidator,
	credentialRevocations *authn.CredentialRevocations,
	delegationTokensSpent keyval.Store,
	simpleStore *simplespace.Store,
	nangoClient *nango.Client,
	mcpGatewayStore mcpgateway.Store,
	llmStore *llmgateway.Store,
	pdsForwarding *forwarding.PDSForwarding,
	clientMetadata *clientmetadata.Resolver,
	searchIndex search.Index,
	searchIndexer *search.Indexer,
	searcher *search.Searcher,
	searchConfigStore *searchconfig.Store,
) *App {
	return &App{
		DB:                    database,
		Domain:                domain,
		HiveDomain:            hiveDomain,
		MCPOrigin:             mcpOrigin,
		OAuthClient:           oauthClient,
		Hive:                  hive,
		DefaultDir:            defaultDir,
		HiveDir:               hiveDir,
		PDSClientFactory:      pdsClientFactory,
		EmailDomainStore:      emailDomainStore,
		HostKey:               hostKey,
		HostKEMKey:            hostKEMKey,
		NotifyStore:           notifyStore,
		Notifier:              notifier,
		BlobBucket:            blobBucket,
		BlobStore:             blobStore,
		SpacesStore:           spacesStore,
		OpensocialStore:       opensocialStore,
		EmailResolver:         emailResolver,
		OAuthServer:           oauthServer,
		ServiceAuth:           serviceAuth,
		PermStore:             permStore,
		Validator:             validator,
		CredentialRevocations: credentialRevocations,
		DelegationTokensSpent: delegationTokensSpent,
		SimpleStore:           simpleStore,
		NangoClient:           nangoClient,
		MCPGatewayStore:       mcpGatewayStore,
		LLMStore:              llmStore,
		PDSForwarding:         pdsForwarding,
		ClientMetadata:        clientMetadata,
		SearchIndex:           searchIndex,
		SearchIndexer:         searchIndexer,
		Searcher:              searcher,
		SearchConfig:          searchConfigStore,
	}
}

// Close releases the resources the App holds.
func (a *App) Close() error { return a.BlobBucket.Close() }
