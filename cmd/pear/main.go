package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"go.opentelemetry.io/contrib/instrumentation/github.com/gorilla/mux/otelmux"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/errgroup"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/habitat-network/habitat/internal/decisions"
	"github.com/habitat-network/habitat/internal/did"
	"github.com/habitat-network/habitat/internal/forwarding"
	"github.com/habitat-network/habitat/internal/httpx"
	habitat_identity "github.com/habitat-network/habitat/internal/identity"
	"github.com/habitat-network/habitat/internal/mcpsearch"
	"github.com/habitat-network/habitat/internal/mcpserver"
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/reranker"
	"go.opentelemetry.io/otel/trace"

	"github.com/habitat-network/habitat/internal/log"
	"github.com/habitat-network/habitat/internal/pearserver"
	"github.com/habitat-network/habitat/internal/telemetry"
	"github.com/habitat-network/habitat/internal/webui"
	"github.com/urfave/cli/v3"
)

func main() {
	cmd := &cli.Command{
		Flags:    getFlags(),
		Commands: commands(),
	}
	ctx := context.Background()
	if err := cmd.Run(ctx, os.Args); err != nil {
		slog.ErrorContext(ctx, "error running command", "err", err)
		os.Exit(1)
	}
}

func serve(ctx context.Context, cmd *cli.Command) error {
	port := cmd.String(fPort)
	httpsCerts := cmd.String(fHttpsCerts)

	notifyCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Setup OpenTelemetry
	// This needs to happen at the beginning so components use the global logger initialized below
	// by slog.
	otelClose, err := telemetry.SetupOpenTelemetry(notifyCtx, "pear")
	defer func() { _ = otelClose(context.Background()) }()
	if err != nil {
		return fmt.Errorf("setup open telemetry for metric/trace/log collection: %w", err)
	}
	slog.InfoContext(notifyCtx, "successfully set up open telemetry")

	tracer := otel.Tracer("pear/main")
	startupCtx, startupSpan := tracer.Start(notifyCtx, "startup")

	meter := otel.Meter("habitat-meter")

	gauge, err := meter.Int64Gauge("habitat.running", metric.WithUnit("item"))
	if err != nil {
		slog.ErrorContext(startupCtx, "failed to create gauge", "err", err)
	} else {
		gauge.Record(startupCtx, 1)
		defer gauge.Record(context.Background(), 0)
	}

	slog.InfoContext(startupCtx, "running with flags", "flags", cmd.FlagNames())

	app, err := setupComponents(startupCtx, cmd)
	if err != nil {
		return err
	}
	defer func() { _ = app.Close() }()

	mux := mux.NewRouter()

	mux.Use(otelmux.Middleware("habitat-server", otelmux.WithPublicEndpoint()))
	mux.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			span := trace.SpanFromContext(r.Context())
			span.SetAttributes(attribute.String("http.request.header.referer", r.Referer()))
			next.ServeHTTP(w, r)
		})
	})
	mux.Use(handlers.CORS(
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
			"Signature",
			"Signature-Input",
			"atproto-space-audience",
			"atproto-space-want-mac",
		}),
		handlers.MaxAge(86400),
		// Browser syncers can only verify a commit or record MAC they can read.
		handlers.ExposedHeaders([]string{"DPoP-Nonce", "Atproto-Space-Mac"}),
	))
	if cmd.Bool(fDebug) {
		mux.Use(func(next http.Handler) http.Handler {
			return handlers.LoggingHandler(os.Stdout, next)
		})
	}

	// Canonical liveness endpoint. Registered before any auth-gated routes so
	// it stays reachable without credentials; used by deploy healthchecks and
	// the startup smoke test.
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Implement service proxying https://atproto.com/specs/xrpc#service-proxying
	mux.Use(forwarding.NewServiceProxy(app.Validator, app.Hive, app.HiveDir, app.PDSClientFactory))

	// Consolidated server owning the opensocial, simplespace, relationship,
	// spaces, and registerNotify handler routes.
	pearApp := pearserver.New(app)

	// PearServer owns a large slice of the XRPC surface. Mounting it as a
	// matcher rather than under path prefixes means the routes it serves are
	// declared once, in internal/pearserver/routes.go, instead of being
	// duplicated here and drifting out of sync. Anything PearServer does not
	// route falls through to the rest of this mux.
	mux.MatcherFunc(pearApp.Handles).Handler(pearApp)

	// A nil *search.Searcher in an interface would be non-nil, so only set it
	// when search is configured.
	var mcpSearcher mcpserver.RecordSearcher
	if app.Searcher != nil {
		mcpSearcher = app.Searcher
	}
	var mcpOpts []mcpserver.Option
	if cmd.Bool(fMCPTextOverrides) {
		mcpOpts = append(mcpOpts, mcpserver.WithTextOverrides())
	}
	mcpOpts = append(mcpOpts, mcpserver.WithFederatedSearcher(mcpsearch.NewSearcher(
		app.MCPGatewayStore, mcpsearch.NewRegistry(mcpsearch.DefaultProviders()...),
	)))
	if u := cmd.String(fDecisionsURL); u != "" {
		mcpOpts = append(mcpOpts, mcpserver.WithToolSelector(mcpserver.DecisionSelector{
			Chooser: decisions.New(
				u, cmd.String(fDecisionsAPIKey), cmd.String(fDecisionsModel), httpx.NewClient(),
			),
		}))
	}
	if key := cmd.String(fRerankAPIKey); key != "" {
		model := cmd.String(fRerankModel)
		if model == "" {
			return fmt.Errorf("--%s is required when --%s is set", fRerankModel, fRerankAPIKey)
		}
		mcpOpts = append(mcpOpts, mcpserver.WithReranker(
			reranker.NewCohere(cmd.String(fRerankURL), key, model, httpx.NewClient()),
		))
	}
	mcpServer := mcpserver.New(
		app.OAuthServer,
		app.SpacesStore,
		app.PermStore,
		mcpSearcher,
		app.MCPGatewayStore,
		app.BlobStore,
		app.HiveDir,
		app.MCPOrigin,
		app.OAuthServer.MCPIssuer(),
		mcpOpts...,
	)

	// Server for opensocial community routes
	mux.HandleFunc("/xrpc/network.habitat.emaildomain.createOrg", pearApp.CreateEmailDomainOrg)
	// Server-side client-metadata proxy for the management frontend
	mux.HandleFunc("/client-metadata", pearApp.GetClientMetadata)

	idServer, err := habitat_identity.NewServer(
		app.Hive, app.Validator, app.PDSForwarding, app.Domain,
		habitat_identity.WithClient(httpx.NewClient()),
		habitat_identity.WithEmailResolver(app.EmailResolver),
	)
	if err != nil {
		return fmt.Errorf("setup hive server: %w", err)
	}

	mux.Host("{opaqueID:.+}." + app.HiveDomain).
		Path("/.well-known/did.json").
		HandlerFunc(idServer.ServeDIDDoc)
	mux.Host("{handle:.+}." + app.HiveDomain).
		Path("/.well-known/atproto-did").
		HandlerFunc(idServer.ServeHandle)
	mux.Headers(habitat_identity.HabitatHostHeader, "").
		Path("/.well-known/did.json").
		HandlerFunc(idServer.ServeDIDDoc)
	mux.Headers(habitat_identity.HabitatHostHeader, "").
		Path("/.well-known/atproto-did").
		HandlerFunc(idServer.ServeHandle)

	hostPublicKey, err := app.HostKey.PublicKey()
	if err != nil {
		return fmt.Errorf("failed to get host public key: %w", err)
	}
	hostKEMPublicKey, err := app.HostKEMKey.PublicKey()
	if err != nil {
		return fmt.Errorf("failed to get host kem public key: %w", err)
	}
	mux.Handle("/.well-known/did.json", did.NewHandler(
		// The host resolves like any other space authority: #atproto signs
		// its space credentials and #atproto_pds is where its spaces are served.
		did.Web(app.Domain).
			AtprotoKey(hostPublicKey.Multibase()).
			ATProtoKEMKey(hostKEMPublicKey.Multibase()).
			HabitatKey(hostPublicKey.Multibase()).
			Habitat("https://"+app.Domain).
			ATProtoPDS("https://"+app.Domain).
			Build(),
	))
	mux.HandleFunc("/client-metadata.json", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(r.Context(), w, app.OAuthClient.ClientMetadata())
	})

	mux.HandleFunc(
		"/.well-known/oauth-authorization-server",
		app.OAuthServer.HandleAuthServerMetadata,
	)
	mux.HandleFunc(
		"/.well-known/oauth-protected-resource",
		app.OAuthServer.HandleProtectedResourceMetadata,
	)

	mux.HandleFunc("/oauth-callback", app.OAuthServer.HandleCallback)
	mux.HandleFunc("/oauth/authorize", app.OAuthServer.HandleAuthorize)
	mux.HandleFunc("/oauth/par", app.OAuthServer.HandlePAR)
	mux.HandleFunc("/oauth/consent", app.OAuthServer.HandleConsent)
	mux.HandleFunc("/oauth/opensocial", app.OAuthServer.HandleOpensocial)
	mux.HandleFunc("/oauth/token", app.OAuthServer.HandleToken)
	mux.HandleFunc("/oauth/forget-login", app.OAuthServer.HandleForgetLogin).Methods("POST")
	mux.HandleFunc("/xrpc/network.habitat.listConnectedApps", app.OAuthServer.ListConnectedApps)

	// MCP (Model Context Protocol) server. Its endpoints match what MCP
	// clients expect (dynamic client registration, no PAR) but share
	// oauthServer's provider, storage, and sign-in with the atproto endpoints
	// above — see internal/oauthserver/mcp.go.
	mux.HandleFunc(oauthserver.MCPMetadataPath, app.OAuthServer.HandleMCPMetadata)
	mux.HandleFunc(oauthserver.MCPRegisterPath, app.OAuthServer.HandleMCPRegister).Methods("POST")
	mux.HandleFunc(oauthserver.MCPAuthorizePath, app.OAuthServer.HandleMCPAuthorize).Methods("GET")
	mux.HandleFunc(oauthserver.MCPAuthorizeSubmitPath, app.OAuthServer.HandleMCPAuthorizeSubmit).
		Methods("POST")
	// MCP-issued tokens share the atproto endpoints' token handler (see
	// OAuthServer.HandleToken); it tells the two kinds of client apart itself.
	mux.HandleFunc(oauthserver.MCPTokenPath, app.OAuthServer.HandleToken).Methods("POST")
	mux.PathPrefix(mcpserver.ProtectedResourceMetadataPath + "/").
		Handler(mcpServer.ProtectedResourceMetadataHandler())
	mux.PathPrefix(mcpserver.Path + "/").Handler(mcpServer.Handler())

	// The methods PearServer does not register are forwarded to the PDS that
	// hosts them; it is mounted after the PearServer matcher above, which is
	// what lets com.atproto.repo.uploadBlob be served locally instead.
	mux.PathPrefix("/xrpc/com.atproto.repo.").Handler(app.PDSForwarding)
	mux.PathPrefix("/xrpc/com.atproto.sync.").Handler(app.PDSForwarding)

	mux.HandleFunc("/xrpc/com.atproto.server.getServiceAuth", idServer.GetServiceAuth)
	mux.HandleFunc("/xrpc/com.atproto.identity.resolveDid", idServer.ResolveDID)
	mux.HandleFunc("/xrpc/com.atproto.identity.resolveHandle", idServer.ResolveHandle)
	mux.HandleFunc("/xrpc/com.atproto.identity.resolveIdentity", idServer.ResolveIdentity)

	uiHandler, err := webui.New(cmd.String(fUiDevProxy))
	if err != nil {
		return fmt.Errorf("setup embedded UI handler: %w", err)
	}
	mux.PathPrefix("/ui/").Handler(uiHandler)

	// gorilla/mux only runs middleware for requests that match a route, so an
	// XRPC method pear has no handler for (e.g. an app's own NSID reached via
	// Atproto-Proxy) would skip both the CORS and service-proxy middleware: the
	// browser's preflight gets a bare 404 with no Access-Control-Allow-Origin.
	// This catch-all, registered last, makes those requests match a route so the
	// middleware runs; unproxied requests still 404.
	mux.PathPrefix("/xrpc/").HandlerFunc(http.NotFound)

	startupSpan.End()
	slog.SetDefault(log.New(log.WithStdout(cmd.Bool(fDebug))))

	s := &http.Server{
		Handler: mux,
		Addr:    fmt.Sprintf(":%s", port),
	}

	eg, egCtx := errgroup.WithContext(startupCtx)
	// Retries the notifySpaceDeleted deliveries that haven't succeeded yet.
	eg.Go(func() error {
		return app.Notifier.Run(egCtx, app.SpacesStore)
	})
	if app.SearchIndexer != nil {
		eg.Go(func() error {
			return app.SearchIndexer.Run(egCtx, app.SpacesStore, app.PermStore)
		})
	}
	eg.Go(func() error {
		slog.InfoContext(egCtx, "starting server", "port", port)
		if httpsCerts == "" {
			return s.ListenAndServe()
		}
		return s.ListenAndServeTLS(
			filepath.Join(httpsCerts, "fullchain.pem"),
			filepath.Join(httpsCerts, "privkey.pem"),
		)
	})

	eg.Go(func() error {
		<-egCtx.Done()
		slog.InfoContext(egCtx, "shutting down server")
		if err := s.Shutdown(context.Background()); err != nil {
			slog.ErrorContext(egCtx, "error shutting down http server", "err", err)
		}
		return nil
	})

	err = eg.Wait()
	if !errors.Is(err, context.Canceled) {
		slog.ErrorContext(startupCtx, "server shut down returned an error", "err", err)
	}
	return err
}
