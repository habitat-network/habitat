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
	"github.com/habitat-network/habitat/internal/clique"
	"github.com/habitat-network/habitat/internal/emaildomain"
	"github.com/habitat-network/habitat/internal/forwarding"
	"github.com/habitat-network/habitat/internal/hive"
	habitat_identity "github.com/habitat-network/habitat/internal/identity"
	"github.com/habitat-network/habitat/internal/instance"
	"github.com/habitat-network/habitat/internal/llmgateway"
	"github.com/habitat-network/habitat/internal/login"
	"github.com/habitat-network/habitat/internal/mcpgateway"
	"github.com/habitat-network/habitat/internal/nango"
	"github.com/habitat-network/habitat/internal/notify"
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/internal/perms"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/searchconfig"
	"github.com/habitat-network/habitat/internal/simplespace"
	"github.com/habitat-network/habitat/internal/spaces"
)

// InstanceStore is the instance admin store as the instance server and the org
// server each use it.
type InstanceStore interface {
	instance.AdminStore
	instance.PolicyStore
}

// App holds pear's stores and services.
type App struct {
	DB                  *gorm.DB
	InstanceAdminStore  InstanceStore
	InstanceAdminServer *instance.Server
	Domain              string
	HiveDomain          string
	MCPOrigin           string
	OAuthClient         pdsclient.PdsOAuthClient
	Hive                hive.Hive
	DefaultDir          identity.Directory
	HiveDir             identity.Directory
	PDSClientFactory    pdsclient.HttpClientFactory
	PasswordProvider    *login.PasswordLoginProvider
	OrgStore            org.Store
	EmailDomainStore    *emaildomain.Store
	HostKey             atcrypto.PrivateKey
	NotifyStore         notify.Store
	BlobBucket          *blob.Bucket
	BlobStore           spaces.BlobStore
	SpacesStore         spaces.Store
	OpensocialStore     *opensocial.Store
	EmailResolver       *habitat_identity.EmailResolver
	OAuthServer         *oauthserver.OAuthServer
	ServiceAuth         *authn.AtprotoServiceAuthMethod
	CliqueStore         clique.Store
	PermStore           perms.Store
	Validator           authn.RequestValidator
	SimpleStore         *simplespace.Store
	NangoClient         *nango.Client
	MCPGatewayStore     mcpgateway.Store
	LLMStore            *llmgateway.Store
	PDSForwarding       *forwarding.PDSForwarding
	ClientMetadata      *clientmetadata.Resolver
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
	instanceAdminStore InstanceStore,
	instanceAdminServer *instance.Server,
	domain string,
	hiveDomain string,
	mcpOrigin string,
	oauthClient pdsclient.PdsOAuthClient,
	hive hive.Hive,
	defaultDir identity.Directory,
	hiveDir identity.Directory,
	pdsClientFactory pdsclient.HttpClientFactory,
	passwordProvider *login.PasswordLoginProvider,
	orgStore org.Store,
	emailDomainStore *emaildomain.Store,
	hostKey atcrypto.PrivateKey,
	notifyStore notify.Store,
	blobBucket *blob.Bucket,
	blobStore spaces.BlobStore,
	spacesStore spaces.Store,
	opensocialStore *opensocial.Store,
	emailResolver *habitat_identity.EmailResolver,
	oauthServer *oauthserver.OAuthServer,
	serviceAuth *authn.AtprotoServiceAuthMethod,
	cliqueStore clique.Store,
	permStore perms.Store,
	validator authn.RequestValidator,
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
		DB:                  database,
		InstanceAdminStore:  instanceAdminStore,
		InstanceAdminServer: instanceAdminServer,
		Domain:              domain,
		HiveDomain:          hiveDomain,
		MCPOrigin:           mcpOrigin,
		OAuthClient:         oauthClient,
		Hive:                hive,
		DefaultDir:          defaultDir,
		HiveDir:             hiveDir,
		PDSClientFactory:    pdsClientFactory,
		PasswordProvider:    passwordProvider,
		OrgStore:            orgStore,
		EmailDomainStore:    emailDomainStore,
		HostKey:             hostKey,
		NotifyStore:         notifyStore,
		BlobBucket:          blobBucket,
		BlobStore:           blobStore,
		SpacesStore:         spacesStore,
		OpensocialStore:     opensocialStore,
		EmailResolver:       emailResolver,
		OAuthServer:         oauthServer,
		ServiceAuth:         serviceAuth,
		CliqueStore:         cliqueStore,
		PermStore:           permStore,
		Validator:           validator,
		SimpleStore:         simpleStore,
		NangoClient:         nangoClient,
		MCPGatewayStore:     mcpGatewayStore,
		LLMStore:            llmStore,
		PDSForwarding:       pdsForwarding,
		ClientMetadata:      clientMetadata,
		SearchIndex:         searchIndex,
		SearchIndexer:       searchIndexer,
		Searcher:            searcher,
		SearchConfig:        searchConfigStore,
	}
}

// Close releases the resources the App holds.
func (a *App) Close() error { return a.BlobBucket.Close() }
