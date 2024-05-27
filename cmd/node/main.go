package main

import (
	"github.com/eagraf/habitat-new/internal/node/api"
	"github.com/eagraf/habitat-new/internal/node/config"
	"github.com/eagraf/habitat-new/internal/node/constants"
	"github.com/eagraf/habitat-new/internal/node/controller"
	"github.com/eagraf/habitat-new/internal/node/docker"
	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/eagraf/habitat-new/internal/node/hdb/hdbms"
	"github.com/eagraf/habitat-new/internal/node/logging"
	"github.com/eagraf/habitat-new/internal/node/package_manager"
	"github.com/eagraf/habitat-new/internal/node/processes"
	"github.com/eagraf/habitat-new/internal/node/pubsub"
	"github.com/eagraf/habitat-new/internal/node/reverse_proxy"
)

func main() {
	log := logging.NewLogger()

	nodeConfig, err := config.NewNodeConfig()
	if err != nil {
		log.Fatal().Err(err)
	}

	hdbPublisher := pubsub.NewSimplePublisher[hdb.StateUpdate]()
	db, dbClose, err := hdbms.NewHabitatDB(log, hdbPublisher, nodeConfig)
	if err != nil {
		log.Fatal().Err(err)
	}
	defer dbClose()

	nodeCtrl, err := controller.NewNodeController(db.Manager, nodeConfig)
	if err != nil {
		log.Fatal().Err(err)
	}

	routes := []api.Route{
		api.NewVersionHandler(),
		controller.NewGetNodeRoute(db.Manager),
		controller.NewAddUserRoute(nodeCtrl),
		controller.NewInstallAppRoute(nodeCtrl),
		controller.NewStartProcessHandler(nodeCtrl),
		controller.NewMigrationRoute(nodeCtrl),
	}

	router := api.NewRouter(routes, log, nodeCtrl, nodeConfig)
	proxy := reverse_proxy.NewProxyServer(log, nodeConfig)
	tlsConfig, err := nodeConfig.TLSConfig()
	if err != nil {
		log.Fatal().Err(err)
	}
	proxyClose, err := proxy.Start(constants.DefaultPortReverseProxy, tlsConfig)
	if err != nil {
		log.Fatal().Err(err)
	}
	defer proxyClose()

	dockerDriver, err := docker.NewDockerDriver()
	if err != nil {
		log.Fatal().Err(err)
	}

	stateLogger := hdbms.NewStateUpdateLogger(log)
	appLifecycleSubscriber, err := package_manager.NewAppLifecycleSubscriber(dockerDriver.PackageManager, nodeCtrl)
	if err != nil {
		log.Fatal().Err(err)
	}

	pm := processes.NewProcessManager([]processes.ProcessDriver{dockerDriver.ProcessDriver})
	pmSub, err := processes.NewProcessManagerStateUpdateSubscriber(pm, nodeCtrl)
	if err != nil {
		log.Fatal().Err(err)
	}

	stateUpdates := pubsub.NewSimpleChannel([]pubsub.Publisher[hdb.StateUpdate]{hdbPublisher}, []pubsub.Subscriber[hdb.StateUpdate]{stateLogger, appLifecycleSubscriber, pmSub})
	go func() {
		err := stateUpdates.Listen()
		if err != nil {
			log.Fatal().Err(err).Msgf("unrecoverable error listening to channel")
		}
	}()

	err = nodeCtrl.InitializeNodeDB()
	if err != nil {
		log.Fatal().Err(err)
	}

	server, err := api.NewAPIServer(router, log, proxy.Rules, nodeConfig)
	if err != nil {
		log.Fatal().Err(err)
	}
	defer server.Close()
	err = server.ListenAndServeTLS(nodeConfig.NodeCertPath(), nodeConfig.NodeKeyPath())
	log.Fatal().Msgf("Habitat API server error: %s", err)
}
