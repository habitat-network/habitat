package pearserver

import (
	"net/http"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/gorilla/mux"
	"github.com/gorilla/schema"

	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/clientmetadata"
	"github.com/habitat-network/habitat/internal/emaildomain"
	"github.com/habitat-network/habitat/internal/forwarding"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/llmgateway"
	"github.com/habitat-network/habitat/internal/mcpgateway"
	"github.com/habitat-network/habitat/internal/notify"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/pearapp"
	"github.com/habitat-network/habitat/internal/perms"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/searchconfig"
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

	// dir resolves identities named by the service identifiers
	// registerNotify accepts, to find the endpoint to deliver to.
	dir identity.Directory

	hive      hive.Hive
	hostKey   atcrypto.PrivateKey
	blobStore spaces.BlobStore

	spacesStore     spaces.Store
	opensocialStore *opensocial.Store
	permStore       perms.Store
	simpleStore     *simplespace.Store
	notifyStore     notify.Store
	mcpGatewayStore mcpgateway.Store

	// llmStore holds each org's LLM model connections.
	llmStore *llmgateway.Store

	// clientMeta resolves OAuth client metadata/JWKS for verifying client
	// attestations on getSpaceCredential.
	clientMeta *clientmetadata.Resolver

	// pdsForwarding proxies requests for remote identities to their real PDS.
	pdsForwarding *forwarding.PDSForwarding

	// emailDomainStore maps email domains to orgs for email-based sign-in.
	emailDomainStore *emaildomain.Store

	// searcher answers searchRecords.
	searcher *search.Searcher

	// searchConfig stores the collections each org surfaces in search.
	searchConfig *searchconfig.Store
}

// New creates a PearServer with the given dependencies and prepares
// an internal router for future handler registration.
func New(app *pearapp.App) *PearServer {
	ps := &PearServer{
		router:           mux.NewRouter(),
		domain:           app.Domain,
		validator:        app.Validator,
		decoder:          schema.NewDecoder(),
		dir:              app.DefaultDir,
		hive:             app.Hive,
		hostKey:          app.HostKey,
		blobStore:        app.BlobStore,
		spacesStore:      app.SpacesStore,
		opensocialStore:  app.OpensocialStore,
		permStore:        app.PermStore,
		simpleStore:      app.SimpleStore,
		notifyStore:      app.NotifyStore,
		clientMeta:       app.ClientMetadata,
		mcpGatewayStore:  app.MCPGatewayStore,
		llmStore:         app.LLMStore,
		pdsForwarding:    app.PDSForwarding,
		emailDomainStore: app.EmailDomainStore,
		searcher:         app.Searcher,
		searchConfig:     app.SearchConfig,
	}
	ps.registerRoutes()
	return ps
}

// ServeHTTP implements http.Handler.
func (p *PearServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.router.ServeHTTP(w, r)
}

// Handles reports whether PearServer's own router has a route for r. It is
// meant to be used as a mux.MatcherFunc so a parent router delegates exactly
// the requests PearServer owns, and passes everything else along to its own
// routes, instead of mounting PearServer under a handful of path prefixes that
// have to be kept in sync with registerRoutes.
//
// On a match it also records the inner route's path template as r.Pattern. The
// parent route is a bare matcher with no path template, so otel's mux
// middleware, which runs after matching, would otherwise find no route and
// name every span "HTTP <method> route not found". The parent router copies r
// after matching, so the value set here reaches the middleware.
func (p *PearServer) Handles(r *http.Request, _ *mux.RouteMatch) bool {
	var match mux.RouteMatch
	if !p.router.Match(r, &match) {
		return false
	}
	if match.Route != nil {
		if tmpl, err := match.Route.GetPathTemplate(); err == nil {
			r.Pattern = tmpl
		}
	}
	return true
}
