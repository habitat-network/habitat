package pearserver

import (
	"net/http"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/gorilla/mux"
	"github.com/gorilla/schema"

	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/clientmetadata"
	"github.com/habitat-network/habitat/internal/forwarding"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/mcpgateway"
	"github.com/habitat-network/habitat/internal/notify"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/perms"
	"github.com/habitat-network/habitat/internal/simplespace"
	"github.com/habitat-network/habitat/internal/spaces"
)

// PearServer is the consolidated HTTP server for habitat.
// It will own all handler methods and register routes internally.
type PearServer struct {
	router *mux.Router

	domain string

	validator authn.RequestValidator
	decoder   *schema.Decoder

	hive      hive.Hive
	hostKey   atcrypto.PrivateKey
	blobStore spaces.BlobStore

	spacesStore     spaces.Store
	opensocialStore *opensocial.Store
	permStore       perms.Store
	simpleStore     *simplespace.Store
	notifyStore     notify.Store
	mcpGatewayStore mcpgateway.Store

	// clientMeta resolves OAuth client metadata/JWKS for verifying client
	// attestations on getSpaceCredential.
	clientMeta *clientmetadata.Resolver

	// pdsForwarding proxies requests for remote identities to their real PDS.
	pdsForwarding *forwarding.PDSForwarding
}

// New creates a PearServer with the given dependencies and prepares
// an internal router for future handler registration.
func New(
	domain string,
	validator authn.RequestValidator,
	hive hive.Hive,
	hostKey atcrypto.PrivateKey,
	blobStore spaces.BlobStore,
	spacesStore spaces.Store,
	opensocialStore *opensocial.Store,
	permStore perms.Store,
	simpleStore *simplespace.Store,
	notifyStore notify.Store,
	clientMeta *clientmetadata.Resolver,
	mcpGatewayStore mcpgateway.Store,
	pdsForwarding *forwarding.PDSForwarding,
) *PearServer {
	ps := &PearServer{
		router:          mux.NewRouter(),
		domain:          domain,
		validator:       validator,
		decoder:         schema.NewDecoder(),
		hive:            hive,
		hostKey:         hostKey,
		blobStore:       blobStore,
		spacesStore:     spacesStore,
		opensocialStore: opensocialStore,
		permStore:       permStore,
		simpleStore:     simpleStore,
		notifyStore:     notifyStore,
		clientMeta:      clientMeta,
		mcpGatewayStore: mcpGatewayStore,
		pdsForwarding:   pdsForwarding,
	}
	ps.registerRoutes()
	return ps
}

// ServeHTTP implements http.Handler.
func (p *PearServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.router.ServeHTTP(w, r)
}
