package controller

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/eagraf/habitat-new/internal/node/reverse_proxy"
	"github.com/eagraf/habitat-new/internal/package_manager"
	"github.com/eagraf/habitat-new/internal/process"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"golang.org/x/mod/semver"
)

type Controller2 struct {
	ctx            context.Context
	db             hdb.Client
	processManager process.ProcessManager
	pkgManagers    map[node.DriverType]package_manager.PackageManager
	proxyServer    *reverse_proxy.ProxyServer
	pdsURL         string
}

func NewController2(
	ctx context.Context,
	processManager process.ProcessManager,
	pkgManagers map[node.DriverType]package_manager.PackageManager,
	db hdb.Client,
	proxyServer *reverse_proxy.ProxyServer,
	pdsURL string,
) (*Controller2, error) {
	// Validate types of all input components
	_, ok := processManager.(node.Component[process.RestoreInfo])
	if !ok {
		return nil, fmt.Errorf("Process manager of type %T does not implement Component[*node.Process]", processManager)
	}

	ctrl := &Controller2{
		ctx:            ctx,
		processManager: processManager,
		pkgManagers:    pkgManagers,
		db:             db,
		proxyServer:    proxyServer,
		pdsURL:         pdsURL,
	}

	return ctrl, nil
}

func (c *Controller2) getNodeState() (*node.State, error) {
	var nodeState node.State
	err := json.Unmarshal(c.db.Bytes(), &nodeState)
	if err != nil {
		return nil, err
	}
	return &nodeState, nil
}

func (c *Controller2) startProcess(installationID string) error {
	state, err := c.getNodeState()
	if err != nil {
		return fmt.Errorf("error getting node state: %s", err.Error())
	}

	app, ok := state.AppInstallations[installationID]
	if !ok {
		return fmt.Errorf("app with ID %s not found", installationID)
	}

	transition, id, err := node.CreateProcessStartTransition(installationID, state)
	if err != nil {
		return errors.Wrap(err, "error creating transition")
	}

	newJSONState, err := c.db.ProposeTransitions([]hdb.Transition{transition})
	if err != nil {
		return errors.Wrap(err, "error proposing transition")
	}

	var newState node.State
	err = newJSONState.Unmarshal(&newState)
	if err != nil {
		return errors.Wrap(err, "error getting new state")
	}

	err = c.processManager.StartProcess(c.ctx, id, app)
	if err != nil {
		// Rollback the state change if the process start failed
		_, err = c.db.ProposeTransitions([]hdb.Transition{
			node.CreateProcessStopTransition(id),
		})
		return errors.Wrap(err, "error starting process")
	}

	// Register with reverse proxy server
	for _, rule := range newState.ReverseProxyRules {
		if rule.AppID == app.ID {
			if c.proxyServer.RuleSet.AddRule(rule) != nil {
				return errors.Wrap(err, "error adding reverse proxy rule")
			}
		}
	}

	return nil
}

func (c *Controller2) stopProcess(processID node.ProcessID) error {
	procErr := c.processManager.StopProcess(c.ctx, processID)
	// If there was no process found with this ID, continue with the state transition
	// Otherwise this action failed, return an error without the transition
	if procErr != nil && !errors.Is(procErr, process.ErrNoProcFound) {
		// process.ErrNoProcFound is sometimes expected. In this case, still
		// attempt to remove the process from the node state.
		return procErr
	}

	// Only propose transitions if the process exists in state
	_, err := c.db.ProposeTransitions([]hdb.Transition{
		node.CreateProcessStopTransition(processID),
	})
	return err
}

func (c *Controller2) installApp(userID string, pkg *node.Package, version string, name string, proxyRules []*node.ReverseProxyRule, start bool) error {
	installer, ok := c.pkgManagers[pkg.Driver]
	if !ok {
		return fmt.Errorf("No driver %s found for app installation [name: %s, version: %s, package: %v]", pkg.Driver, name, version, pkg)
	}

	transition, id := node.CreateStartInstallationTransition(userID, pkg, version, name, proxyRules)
	_, err := c.db.ProposeTransitions([]hdb.Transition{
		transition,
	})
	if err != nil {
		return err
	}

	err = installer.InstallPackage(pkg, version)
	if err != nil {
		return err
	}
	_, err = c.db.ProposeTransitions([]hdb.Transition{
		node.CreateFinishInstallationTransition(id),
	})
	if err != nil {
		return err
	}

	if start {
		return c.startProcess(id)
	}
	return nil
}

func (c *Controller2) uninstallApp(appID string) error {
	_, err := c.db.ProposeTransitions([]hdb.Transition{
		node.CreateUninstallAppTransition(appID),
	})
	return err
}

func (c *Controller2) addUser(ctx context.Context, input *atproto.ServerCreateAccount_Input) (*atproto.ServerCreateAccount_Output, error) {
	output, err := atproto.ServerCreateAccount(
		ctx,
		&xrpc.Client{
			Host: c.pdsURL, // xrpc.Client Host param expects url
		},
		input,
	)
	if err != nil {
		return nil, err
	}

	_, err = c.db.ProposeTransitions([]hdb.Transition{
		node.CreateAddUserTransition(output.Handle, output.Did),
	})
	if err != nil {
		return nil, err
	}
	return output, nil
}

func (c *Controller2) migrateDB(targetVersion string) error {
	var nodeState node.State
	err := json.Unmarshal(c.db.Bytes(), &nodeState)
	if err != nil {
		return nil
	}
	// No-op if version is already the target
	if semver.Compare(nodeState.SchemaVersion, targetVersion) == 0 {
		return nil
	}

	_, err = c.db.ProposeTransitions([]hdb.Transition{
		node.CreateMigrationTransition(targetVersion),
	})
	return err
}

func (c *Controller2) restore(state *node.State) error {
	// Restore app installations to desired state
	for _, pkgManager := range c.pkgManagers {
		err := pkgManager.RestoreFromState(c.ctx, state.AppInstallations)
		if err != nil {
			return err
		}
	}

	// Restore reverse proxy rules to the desired state
	for _, rule := range state.ReverseProxyRules {
		log.Info().Msgf("Restoring rule %s, matcher: %s", rule.ID, rule.Matcher)
		err := c.proxyServer.RuleSet.AddRule(rule)
		if err != nil {
			log.Error().Msgf("error restoring rule: %s", err)
		}
	}

	// Restore processes to the current state
	info := make(map[node.ProcessID]*node.AppInstallation)
	for _, proc := range state.Processes {
		app, ok := state.AppInstallations[proc.AppID]
		if !ok {
			return fmt.Errorf("no app installation found for desired process: ID=%s appID=%s", proc.ID, proc.AppID)
		}
		info[proc.ID] = app
	}

	return c.processManager.RestoreFromState(c.ctx, info)
}
