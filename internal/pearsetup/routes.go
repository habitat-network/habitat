package pearsetup

import (
	"net/http"
	"os"

	"github.com/gorilla/handlers"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gorilla/mux/otelmux"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/habitat-network/habitat/internal/did"
	"github.com/habitat-network/habitat/internal/forwarding"
	"github.com/habitat-network/habitat/internal/httpx"
	habitat_identity "github.com/habitat-network/habitat/internal/identity"
)

// registerMiddleware installs tracing, CORS, optional request logging, the
// liveness endpoint, and AT Proto service proxying, in that order. /health is
// registered here — before any auth-gated route — so it stays reachable
// without credentials; the service proxy middleware is installed before any
// route is matched.
func (p *Pear) registerMiddleware() {
	p.Router.Use(otelmux.Middleware("habitat-server", otelmux.WithPublicEndpoint()))
	p.Router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			span := trace.SpanFromContext(r.Context())
			span.SetAttributes(attribute.String("http.request.header.referer", r.Referer()))
			next.ServeHTTP(w, r)
		})
	})
	p.Router.Use(handlers.CORS(
		handlers.AllowedOrigins([]string{"*"}),
		handlers.AllowedMethods([]string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}),
		handlers.AllowedHeaders([]string{
			"Content-Type",
			"Authorization",
			"habitat-auth-method",
			"User-Agent",
			"atproto-accept-labelers",
			"atproto-proxy",
			"DPoP",
		}),
		handlers.MaxAge(86400),
		handlers.ExposedHeaders([]string{"DPoP-Nonce"}),
	))
	if p.Config.Debug {
		p.Router.Use(func(next http.Handler) http.Handler {
			return handlers.LoggingHandler(os.Stdout, next)
		})
	}

	// Canonical liveness endpoint. Registered before any auth-gated routes so
	// it stays reachable without credentials; used by deploy healthchecks and
	// the startup smoke test.
	p.Router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Implement service proxying https://atproto.com/specs/xrpc#service-proxying
	p.Router.Use(forwarding.NewServiceProxy(p.Validator, p.Hive, p.Directory, p.pdsClientFactory))
}

// registerRoutes registers every route pear serves.
func (p *Pear) registerRoutes() {
	p.Router.HandleFunc("/xrpc/network.habitat.org.getMetadata", p.orgServer.GetMetadata)
	p.Router.HandleFunc("/xrpc/network.habitat.org.getAdmins", p.orgServer.GetAdmins)
	p.Router.HandleFunc("/xrpc/network.habitat.org.getMembers", p.orgServer.GetMembers)
	p.Router.HandleFunc("/xrpc/network.habitat.org.addAdmin", p.orgServer.AddAdmin)
	p.Router.HandleFunc("/xrpc/network.habitat.org.removeAdmin", p.orgServer.RemoveAdmin)
	p.Router.HandleFunc("/xrpc/network.habitat.org.removeMembers", p.orgServer.RemoveMembers)
	p.Router.HandleFunc("/xrpc/network.habitat.org.downgradeAdmin", p.orgServer.DowngradeAdmin)
	p.Router.HandleFunc("/xrpc/network.habitat.org.issueInviteToken", p.orgServer.IssueInviteToken)
	p.Router.HandleFunc("/xrpc/network.habitat.org.mintMemberIdentity", p.orgServer.MintMemberIdentity)
	p.Router.HandleFunc("/xrpc/network.habitat.org.create", p.orgServer.CreateOrg)

	p.Router.Host("{opaqueID:.+}." + p.Config.HiveDomain).
		Path("/.well-known/did.json").
		HandlerFunc(p.idServer.ServeDIDDoc)
	p.Router.Host("{handle:.+}." + p.Config.HiveDomain).
		Path("/.well-known/atproto-did").
		HandlerFunc(p.idServer.ServeHandle)
	p.Router.Headers(habitat_identity.HabitatHostHeader, "").
		Path("/.well-known/did.json").
		HandlerFunc(p.idServer.ServeDIDDoc)
	p.Router.Headers(habitat_identity.HabitatHostHeader, "").
		Path("/.well-known/atproto-did").
		HandlerFunc(p.idServer.ServeHandle)

	p.Router.HandleFunc("/admin/login", p.instanceServer.ServeLoginPage).Methods("GET")
	p.Router.HandleFunc("/admin/login", p.instanceServer.HandleLogin).Methods("POST")
	p.Router.HandleFunc("/admin/logout", p.instanceServer.HandleLogout).Methods("POST")
	p.Router.HandleFunc("/admin", p.instanceServer.ServeAdminHome).Methods("GET")
	p.Router.HandleFunc("/admin/config", p.instanceServer.ServeConfig).Methods("GET")
	p.Router.HandleFunc("/xrpc/network.habitat.admin.getSettings", p.instanceServer.GetSettings)
	p.Router.HandleFunc("/xrpc/network.habitat.admin.updateSettings", p.instanceServer.UpdateSettings)
	p.Router.HandleFunc("/xrpc/network.habitat.admin.issueInvite", p.instanceServer.IssueInvite)
	p.Router.HandleFunc(
		"/xrpc/network.habitat.instance.describeInstance",
		p.instanceServer.DescribeInstance,
	)

	p.Router.Handle("/.well-known/did.json", did.NewHandler(
		did.Web(p.Config.Domain).
			ATProtoSpaceKey(p.hostPublicKey.Multibase()).
			HabitatKey(p.hostPublicKey.Multibase()).
			Habitat("https://"+p.Config.Domain).
			ATProtoSpaceHost("https://"+p.Config.Domain).
			Build(),
	))
	p.Router.HandleFunc("/client-metadata.json", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(r.Context(), w, p.oauthClient.ClientMetadata())
	})

	p.Router.HandleFunc(
		"/.well-known/oauth-authorization-server",
		p.OAuthServer.HandleAuthServerMetadata,
	)
	p.Router.HandleFunc(
		"/.well-known/oauth-protected-resource",
		p.OAuthServer.HandleProtectedResourceMetadata,
	)

	p.Router.HandleFunc("/oauth-callback", p.OAuthServer.HandleCallback)
	p.Router.HandleFunc("/oauth/authorize", p.OAuthServer.HandleAuthorize)
	p.Router.HandleFunc("/oauth/par", p.OAuthServer.HandlePAR)
	p.Router.HandleFunc("/oauth/consent", p.OAuthServer.HandleConsent)
	p.Router.HandleFunc("/oauth/token", p.OAuthServer.HandleToken)
	p.Router.HandleFunc("/xrpc/network.habitat.listConnectedApps", p.OAuthServer.ListConnectedApps)
	p.Router.HandleFunc("/xrpc/network.habitat.org.loginMember", p.passwordProvider.HandlePasswordLogin)

	p.Router.HandleFunc("/xrpc/network.habitat.repo.putRecord", p.pearServer.PutRecord)
	p.Router.HandleFunc("/xrpc/network.habitat.repo.getRecord", p.pearServer.GetRecord)
	p.Router.HandleFunc("/xrpc/network.habitat.repo.listRecords", p.pearServer.ListRecords)
	p.Router.HandleFunc("/xrpc/network.habitat.repo.describeRepo", p.pearServer.DescribeRepo)
	p.Router.HandleFunc("/xrpc/network.habitat.repo.deleteRecord", p.pearServer.DeleteRecord)
	p.Router.HandleFunc("/xrpc/network.habitat.repo.createRecord", p.pearServer.CreateRecord)
	p.Router.HandleFunc("/xrpc/network.habitat.repo.uploadBlob", p.spacesServer.UploadBlob)

	p.Router.HandleFunc("/xrpc/network.habitat.permissions.listPermissions", p.pearServer.ListPermissions)
	p.Router.HandleFunc("/xrpc/network.habitat.permissions.addPermission", p.pearServer.AddPermission)
	p.Router.HandleFunc("/xrpc/network.habitat.permissions.removePermission",
		p.pearServer.RemovePermission)

	p.Router.HandleFunc("/xrpc/network.habitat.clique.createClique", p.cliqueServer.CreateClique)
	p.Router.HandleFunc("/xrpc/network.habitat.clique.addMembers", p.cliqueServer.AddCliqueMembers)
	p.Router.HandleFunc("/xrpc/network.habitat.clique.removeMembers", p.cliqueServer.RemoveCliqueMembers)
	p.Router.HandleFunc("/xrpc/network.habitat.clique.getMembers", p.cliqueServer.GetCliqueMembers)
	p.Router.HandleFunc("/xrpc/network.habitat.clique.isMember", p.cliqueServer.IsCliqueMember)

	// Spaces
	p.Router.HandleFunc("/xrpc/network.habitat.space.listSpaces", p.spacesServer.ListSpaces)
	p.Router.HandleFunc("/xrpc/network.habitat.space.listRepos", p.spacesServer.ListRepos)
	p.Router.HandleFunc("/xrpc/network.habitat.space.putRecord", p.spacesServer.PutRecord)
	p.Router.HandleFunc("/xrpc/network.habitat.space.getRecord", p.spacesServer.GetRecord)
	p.Router.HandleFunc("/xrpc/network.habitat.space.getBlob", p.spacesServer.GetBlob)
	p.Router.HandleFunc("/xrpc/network.habitat.space.listRecords", p.spacesServer.ListRecords)
	p.Router.HandleFunc("/xrpc/network.habitat.space.deleteRecord", p.spacesServer.DeleteRecord)
	p.Router.HandleFunc("/xrpc/network.habitat.space.listRepoOps", p.spacesServer.ListRepoOps)
	p.Router.HandleFunc("/xrpc/network.habitat.space.getLatestCommit", p.spacesServer.GetLatestCommit)
	p.Router.HandleFunc("/xrpc/network.habitat.space.getRepo", p.spacesServer.GetRepo)
	p.Router.HandleFunc("/xrpc/network.habitat.space.registerNotify", p.notifyServer.RegisterNotify)
	p.Router.HandleFunc("/xrpc/network.habitat.space.getDelegationToken",
		p.spacesServer.GetDelegationToken)
	p.Router.HandleFunc("/xrpc/network.habitat.space.getSpaceCredential",
		p.spacesServer.GetSpaceCredential)

	// Simplespaces
	p.Router.HandleFunc("/xrpc/network.habitat.simplespace.createSpace", p.simplespaceServer.CreateSpace)
	p.Router.HandleFunc("/xrpc/network.habitat.simplespace.addMember", p.simplespaceServer.AddMember)
	p.Router.HandleFunc("/xrpc/network.habitat.simplespace.removeMember", p.simplespaceServer.RemoveMember)
	p.Router.HandleFunc("/xrpc/network.habitat.simplespace.listMembers", p.simplespaceServer.ListMembers)
	p.Router.HandleFunc("/xrpc/network.habitat.simplespace.deleteSpace", p.simplespaceServer.DeleteSpace)

	// Relationships
	p.Router.HandleFunc("/xrpc/network.habitat.relationship.setUserRelation",
		p.relationshipServer.SetUserRelation)
	p.Router.HandleFunc("/xrpc/network.habitat.relationship.setSpaceRelation",
		p.relationshipServer.SetSpaceRelation)
	p.Router.HandleFunc("/xrpc/network.habitat.relationship.deleteRelation",
		p.relationshipServer.DeleteRelation)
	p.Router.HandleFunc("/xrpc/network.habitat.relationship.listRelations",
		p.relationshipServer.ListRelations)
	p.Router.HandleFunc("/xrpc/network.habitat.relationship.checkUserRelation",
		p.relationshipServer.CheckUserRelation)
	p.Router.HandleFunc("/xrpc/network.habitat.relationship.checkSpaceRelation",
		p.relationshipServer.CheckSpaceRelation)
	p.Router.HandleFunc("/xrpc/network.habitat.relationship.resolveRelations",
		p.relationshipServer.ResolveRelations)
	p.Router.HandleFunc("/xrpc/network.habitat.relationship.listRelatedSpaces",
		p.relationshipServer.ListRelatedSpaces)

	p.Router.PathPrefix("/xrpc/com.atproto.repo.").Handler(p.pdsForwarding)
	p.Router.PathPrefix("/xrpc/com.atproto.sync.").Handler(p.pdsForwarding)

	p.Router.HandleFunc("/xrpc/com.atproto.server.getServiceAuth", p.idServer.GetServiceAuth)
	p.Router.HandleFunc("/xrpc/com.atproto.identity.resolveDid", p.idServer.ResolveDID)
	p.Router.HandleFunc("/xrpc/com.atproto.identity.resolveHandle", p.idServer.ResolveHandle)
	p.Router.HandleFunc("/xrpc/com.atproto.identity.resolveIdentity", p.idServer.ResolveIdentity)

	if !p.Config.DisableUI {
		p.Router.PathPrefix("/ui/").Handler(p.uiHandler)
	}
	if !p.Config.DisableP2P {
		p.Router.PathPrefix("/").HandlerFunc(p.p2pServer.HandleLibp2p)
	}
}

// Handler returns the fully routed HTTP handler. Tests serve requests through
// it directly rather than binding a port.
func (p *Pear) Handler() http.Handler {
	return p.Router
}
