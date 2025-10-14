package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	fileadapter "github.com/casbin/casbin/v2/persist/file-adapter"
	"github.com/docker/docker/client"
	"github.com/eagraf/habitat-new/internal/app"
	"github.com/eagraf/habitat-new/internal/auth"
	"github.com/eagraf/habitat-new/internal/docker"
	"github.com/eagraf/habitat-new/internal/node/api"
	"github.com/eagraf/habitat-new/internal/node/appstore"
	"github.com/eagraf/habitat-new/internal/node/config"
	"github.com/eagraf/habitat-new/internal/node/constants"
	"github.com/eagraf/habitat-new/internal/node/controller"
	"github.com/eagraf/habitat-new/internal/node/logging"
	"github.com/eagraf/habitat-new/internal/node/reverse_proxy"
	"github.com/eagraf/habitat-new/internal/node/server"
	"github.com/eagraf/habitat-new/internal/node/state"
	"github.com/eagraf/habitat-new/internal/permissions"
	"github.com/eagraf/habitat-new/internal/privi"
	"github.com/eagraf/habitat-new/internal/process"
	"github.com/eagraf/habitat-new/internal/web"
	"github.com/gorilla/sessions"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

func main() {
	nodeConfig, err := config.NewNodeConfig()
	if err != nil {
		log.Fatal().Err(err).Msg("error loading node config")
	}

	logger := logging.NewLogger()
	zerolog.SetGlobalLevel(nodeConfig.LogLevel())

	dockerClient, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create docker client")
	}
	pm := process.NewProcessManager(
		[]process.Driver{docker.NewDriver(dockerClient), web.NewDriver()},
	)

	// Initialize package managers
	pkgManagers := map[app.DriverType]app.PackageManager{
		app.DriverTypeDocker: docker.NewPackageManager(dockerClient),
		app.DriverTypeWeb:    web.NewPackageManager(nodeConfig.WebBundlePath()),
	}

	if err != nil {
		log.Fatal().Err(err).Msg("error creating app lifecycle subscriber")
	}

	// ctx.Done() returns when SIGINT is called or cancel() is called.
	// calling cancel() unregisters the signal trapping.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// egCtx is cancelled if any function called with eg.Go() returns an error.
	eg, egCtx := errgroup.WithContext(ctx)

	// Generate the list of default proxy rules to have available when the node first comes up
	proxyRules, err := generateDefaultReverseProxyRules(nodeConfig)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to generate proxy rules")
	}

	defaultApps, defaultProxyRules, err := nodeConfig.DefaultApps()
	if err != nil {
		log.Fatal().Err(err).Msg("unable to get default apps")
	}
	rules := append(defaultProxyRules, proxyRules...)

	initState, initialTransitions, err := initialState(
		nodeConfig.RootUserCertB64(),
		defaultApps,
		rules,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to do initial node transitions")
	}

	db, err := state.NewHabitatDB(nodeConfig.HDBPath(), initialTransitions)
	if err != nil {
		log.Fatal().Err(err).Msg("error creating habitat db")
	}

	// Set up the reverse proxy server
	tlsConfig, err := nodeConfig.TLSConfig()
	if err != nil {
		log.Fatal().Err(err).Msg("error getting tls config")
	}
	addr := fmt.Sprintf(":%s", nodeConfig.ReverseProxyPort())

	// Gorilla sessions persisted in the browser's cookies.
	// TODO These need to actually be persisted somewhere
	sessionStoreKey := []byte("FaKe_DeV-SeSsIoN-KeY")

	proxy := reverse_proxy.NewProxyServer(logger, nodeConfig.WebBundlePath())
	proxyServer := &http.Server{
		Addr:    addr,
		Handler: proxy,
	}

	var ln net.Listener
	// If TS_AUTHKEY is set, create a tsnet listener. Otherwise, create a normal tcp listener.
	if nodeConfig.TailscaleAuthkey() == "" {
		ln, err = proxy.Listener(addr)
	} else {
		ln, err = proxy.TailscaleListener(addr, nodeConfig.Hostname(), nodeConfig.TailScaleStatePath(), nodeConfig.TailScaleFunnelEnabled())
	}

	if err != nil {
		log.Fatal().Err(err).Msg("error creating reverse proxy listener")
	}
	eg.Go(server.ServeFn(
		proxyServer,
		"proxy-server",
		server.WithTLSConfig(tlsConfig, nodeConfig.NodeCertPath(), nodeConfig.NodeKeyPath()),
		server.WithListener(ln),
	))

	ctrl2, err := controller.NewController(
		ctx,
		pm,
		pkgManagers,
		db,
		proxy,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("error creating node controller")
	}
	ctrlServer, err := controller.NewCtrlServer(ctx, ctrl2, initState)
	if err != nil {
		log.Fatal().Err(err).Msg("error creating node control server")
	}

	sessionStore := sessions.NewCookieStore(sessionStoreKey)

	// Set up the main API server
	// TODO: create a less tedious way to register all the routes in the future. It might be as simple
	// as having a dedicated file to list these, instead of putting them all in main.
	routes := []api.Route{
		// Node routes
		api.NewVersionHandler(),
	}
	authRoutes, err := auth.GetRoutes(nodeConfig, sessionStore)
	if err != nil {
		log.Fatal().Err(err).Msg("error getting auth routes")
	}
	routes = append(routes, authRoutes...)
	routes = append(routes, ctrlServer.GetRoutes()...)
	if nodeConfig.Environment() == constants.EnvironmentDev {
		// App store is unimplemented in production
		routes = append(routes, appstore.NewAvailableAppsRoute(nodeConfig.HabitatPath()))
	}

	priviServer, priviClose := setupPrivi(nodeConfig)
	defer priviClose()
	routes = append(routes, priviServer.GetRoutes()...)

	router := api.NewRouter(routes, logger)
	apiServer := &http.Server{
		Addr:    fmt.Sprintf(":%s", constants.DefaultPortHabitatAPI),
		Handler: router,
	}
	eg.Go(
		server.ServeFn(
			apiServer,
			"api-server",
			server.WithTLSConfig(tlsConfig, nodeConfig.NodeCertPath(), nodeConfig.NodeKeyPath()),
		),
	)

	// Wait for either os.Interrupt which triggers ctx.Done()
	// Or one of the servers to error, which triggers egCtx.Done()
	select {
	case <-egCtx.Done():
		log.Err(egCtx.Err()).Msg("sub-service errored: shutting down Habitat")
	case <-ctx.Done():
		log.Info().Msg("Interrupt signal received; gracefully closing Habitat")
		stop()
	}

	// Shutdown the API server
	err = apiServer.Shutdown(context.Background())
	if err != nil {
		log.Err(err).Msg("error on api-server shutdown")
	}
	log.Info().Msg("Gracefully shutdown Habitat API server")

	// Shutdown the proxy server
	err = proxyServer.Shutdown(context.Background())
	if err != nil {
		log.Err(err).Msg("error on proxy-server shutdown")
	}
	log.Info().Msg("Gracefully shutdown Habitat proxy server")

	// Wait for the go-routines to finish
	err = eg.Wait()
	if err != nil {
		log.Err(err).Msg("received error on eg.Wait()")
	}
	log.Info().Msg("Finished!")
}

func setupPrivi(nodeConfig *config.NodeConfig) (*privi.Server, func()) {
	policiesDirPath := nodeConfig.PermissionPolicyFilesDir()
	perms, err := permissions.NewStore(
		fileadapter.NewAdapter(filepath.Join(policiesDirPath, "policies.csv")),
		true,
	)
	if err != nil {
		log.Fatal().Err(err).Msgf("error creating permission store")
	}

	// FOR DEMO PURPOSES ONLY
	sashankDID := "did:plc:v3amhno5wvyfams6aioqqj66"
	arushiDID := "did:plc:l3k2mbu6qa6rxjej5tvjj7zz"
	err = perms.AddLexiconReadPermission(arushiDID, sashankDID, "com.habitat.test")
	if err != nil {
		log.Fatal().Err(err).Msgf("error adding test lexicon for sashank demo")
	}

	// Create database file if it does not exist
	priviRepoPath := nodeConfig.PriviRepoFile()
	_, err = os.Stat(priviRepoPath)
	if errors.Is(err, os.ErrNotExist) {
		_, err := os.Create(priviRepoPath)
		if err != nil {
			log.Fatal().Err(err).Msgf("unable to create privi repo file at %s", priviRepoPath)
		}
	} else if err != nil {
		log.Fatal().Err(err).Msgf("error finding privi repo file")
	}

	priviDB, err := sql.Open("sqlite3", priviRepoPath)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to open sqlite file backing privi server")
	}

	repo, err := privi.NewSQLiteRepo(priviDB)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to setup privi sqlite db")
	}

	// Add privy routes
	priviServer := privi.NewServer(
		perms,
		repo,
	)
	return priviServer, func() { priviDB.Close() }
}

func generateDefaultReverseProxyRules(config *config.NodeConfig) ([]*reverse_proxy.Rule, error) {
	frontendRule := &reverse_proxy.Rule{
		ID:      "default-rule-frontend",
		Matcher: "", // Root matcher
	}
	if config.FrontendDev() {
		// In development mode, we run the frontend in a separate docker container with hot-reloading.
		// As a result, all frontend requests must be forwarde to the frontend container.
		frontendRule.Type = reverse_proxy.ProxyRuleRedirect
		frontendRule.Target = "http://habitat_frontend:5173/"
	} else {
		// In production mode, we embed the frontend into the node binary. That way, we can serve
		// the frontend without needing to set it up on the host machine.
		// TODO @eagraf - evaluate the performance implications of this.
		frontendRule.Type = reverse_proxy.ProxyRuleEmbeddedFrontend
	}

	apiURL, err := url.Parse(fmt.Sprintf("http://localhost:%s", constants.DefaultPortHabitatAPI))
	if err != nil {
		return nil, err
	}

	res := []*reverse_proxy.Rule{
		{
			ID:      "default-rule-api",
			Type:    reverse_proxy.ProxyRuleRedirect,
			Matcher: "/habitat/api",
			Target:  apiURL.String(),
		},
		{
			ID:      "habitat-put-record",
			Type:    reverse_proxy.ProxyRuleRedirect,
			Matcher: "/xrpc/com.habitat.putRecord",
			Target:  apiURL.String() + "/xrpc/com.habitat.putRecord",
		},
		{
			ID:      "habitat-get-record",
			Type:    reverse_proxy.ProxyRuleRedirect,
			Matcher: "/xrpc/com.habitat.getRecord",
			Target:  apiURL.String() + "/xrpc/com.habitat.getRecord",
		},
		{
			ID:      "habitat-list-permissions",
			Type:    reverse_proxy.ProxyRuleRedirect,
			Matcher: "/xrpc/com.habitat.listPermissions",
			Target:  apiURL.String() + "/xrpc/com.habitat.listPermissions",
		},
		{
			ID:      "habitat-add-permissions",
			Type:    reverse_proxy.ProxyRuleRedirect,
			Matcher: "/xrpc/com.habitat.addPermission",
			Target:  apiURL.String() + "/xrpc/com.habitat.addPermission",
		},
		{
			ID:      "habitat-remove-permissions",
			Type:    reverse_proxy.ProxyRuleRedirect,
			Matcher: "/xrpc/com.habitat.removePermission",
			Target:  apiURL.String() + "/xrpc/com.habitat.removePermission",
		},
		// Serve a DID document for habitat
		// This rule is currently broken because it clashes with the one above for PDS / OAuth
		// We should delete the PDS side car because we never use it
		{
			ID:      "did-rule",
			Type:    reverse_proxy.ProxyRuleFileServer,
			Matcher: "/.well-known/",
			Target:  config.HabitatPath() + "/well-known/",
		},
		frontendRule,
	}

	// Add any additional reverse proxy rules from the config file
	configRules, err := config.ReverseProxyRules()
	if err != nil {
		return nil, err
	}

	res = append(res, configRules...)

	return res, nil
}

func initialState(
	rootUserCert string,
	startApps []*app.Installation,
	proxyRules []*reverse_proxy.Rule,
) (*state.NodeState, []state.Transition, error) {
	init, err := state.NewStateForLatestVersion()
	if err != nil {
		log.Fatal().Err(err).Msg("unable to generate initial node state")
	}

	init.SetRootUserCert(rootUserCert)
	for _, install := range startApps {
		init.AppInstallations[install.ID] = install
		init.AppInstallations[install.ID].State = app.LifecycleStateInstalled

		procID := process.NewID(install.Driver)
		init.Processes[procID] = &process.Process{
			ID:      procID,
			AppID:   install.ID,
			UserID:  constants.RootUserID,
			Created: time.Now().Format(time.RFC3339),
		}
	}
	for _, rule := range proxyRules {
		init.ReverseProxyRules[rule.ID] = rule
	}

	// A list of transitions to apply when the node starts up for the first time.
	transitions := []state.Transition{state.CreateInitializationTransition(init)}
	return init, transitions, nil
}
