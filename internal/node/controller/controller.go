package controller

import (
	"encoding/json"
	"fmt"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/config"
	"github.com/eagraf/habitat-new/internal/node/constants"
	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/rs/zerolog/log"
	"golang.org/x/mod/semver"
)

// NodeController is an interface to manage common admin actions on a Habitat node.
// For example, installing apps or adding users. This will likely expand to be a much bigger API as we move forward.

type NodeController interface {
	InitializeNodeDB() error
	MigrateNodeDB(targetVersion string) error

	AddUser(userID, username, certificate string) error
	GetUserByUsername(username string) (*node.User, error)

	InstallApp(userID string, newApp *node.AppInstallation, newProxyRules []*node.ReverseProxyRule) error
	FinishAppInstallation(userID string, appID, registryURLBase, registryPackageID string) error
	GetAppByID(appID string) (*node.AppInstallation, error)

	StartProcess(process *node.Process) error
	SetProcessRunning(processID string) error
	StopProcess(processID string) error
}

type BaseNodeController struct {
	databaseManager hdb.HDBManager
	nodeConfig      *config.NodeConfig
}

func NewNodeController(habitatDBManager hdb.HDBManager, config *config.NodeConfig) (*BaseNodeController, error) {
	controller := &BaseNodeController{
		databaseManager: habitatDBManager,
		nodeConfig:      config,
	}
	return controller, nil
}

// InitializeNodeDB tries initializing the database; it is a noop if a database with the same name already exists
func (c *BaseNodeController) InitializeNodeDB() error {

	initialTransitions, err := initTranstitions(c.nodeConfig)
	if err != nil {
		return err
	}

	_, err = c.databaseManager.CreateDatabase(constants.NodeDBDefaultName, node.SchemaName, initialTransitions)
	if err != nil {
		if _, ok := err.(*hdb.DatabaseAlreadyExistsError); ok {
			log.Info().Msg("Node database already exists, doing nothing.")
		} else {
			return err
		}
	}

	return nil
}

func (c *BaseNodeController) MigrateNodeDB(targetVersion string) error {
	dbClient, err := c.databaseManager.GetDatabaseClientByName(constants.NodeDBDefaultName)
	if err != nil {
		return err
	}

	var nodeState node.NodeState
	err = json.Unmarshal(dbClient.Bytes(), &nodeState)
	if err != nil {
		return nil
	}

	// No-op if version is already the target
	if semver.Compare(nodeState.SchemaVersion, targetVersion) == 0 {
		return nil
	}

	_, err = dbClient.ProposeTransitions([]hdb.Transition{
		&node.MigrationTransition{
			TargetVersion: targetVersion,
		},
	})
	return err
}

// InstallApp attempts to install the given app installation, with the userID as the action initiato.
func (c *BaseNodeController) InstallApp(userID string, newApp *node.AppInstallation, newProxyRules []*node.ReverseProxyRule) error {
	dbClient, err := c.databaseManager.GetDatabaseClientByName(constants.NodeDBDefaultName)
	if err != nil {
		return err
	}

	_, err = dbClient.ProposeTransitions([]hdb.Transition{
		&node.StartInstallationTransition{
			UserID:          userID,
			AppInstallation: newApp,
			NewProxyRules:   newProxyRules,
		},
	})
	return err
}

// FinishAppInstallation marks the app lifecycle state as installed
func (c *BaseNodeController) FinishAppInstallation(userID string, appID, registryURLBase, registryAppID string) error {
	dbClient, err := c.databaseManager.GetDatabaseClientByName(constants.NodeDBDefaultName)
	if err != nil {
		return err
	}

	_, err = dbClient.ProposeTransitions([]hdb.Transition{
		&node.FinishInstallationTransition{
			UserID:          userID,
			AppID:           appID,
			RegistryURLBase: registryURLBase,
			RegistryAppID:   registryAppID,
		},
	})
	if err != nil {
		return err
	}

	return nil
}

func (c *BaseNodeController) GetAppByID(appID string) (*node.AppInstallation, error) {
	dbClient, err := c.databaseManager.GetDatabaseClientByName(constants.NodeDBDefaultName)
	if err != nil {
		return nil, err
	}

	var nodeState node.NodeState
	err = json.Unmarshal(dbClient.Bytes(), &nodeState)
	if err != nil {
		return nil, err
	}

	app, ok := nodeState.AppInstallations[appID]
	if !ok {
		return nil, fmt.Errorf("app with ID %s not found", appID)
	}

	return app.AppInstallation, nil
}

func (c *BaseNodeController) StartProcess(process *node.Process) error {
	dbClient, err := c.databaseManager.GetDatabaseClientByName(constants.NodeDBDefaultName)
	if err != nil {
		return err
	}

	var nodeState node.NodeState
	err = json.Unmarshal(dbClient.Bytes(), &nodeState)
	if err != nil {
		return nil
	}

	var user *node.User
	for _, u := range nodeState.Users {
		if u.ID == process.UserID {
			user = u
		}
	}
	if user == nil {
		return fmt.Errorf("user %s not found", process.UserID)
	}

	var app *node.AppInstallation
	appState, ok := nodeState.AppInstallations[process.AppID]
	if !ok {
		return fmt.Errorf("app with ID %s not found", process.AppID)
	}
	app = appState.AppInstallation

	_, err = dbClient.ProposeTransitions([]hdb.Transition{
		&node.ProcessStartTransition{
			Process: process,
			App:     app,
		},
	})
	if err != nil {
		return err
	}

	return nil
}

func (c *BaseNodeController) SetProcessRunning(processID string) error {
	dbClient, err := c.databaseManager.GetDatabaseClientByName(constants.NodeDBDefaultName)
	if err != nil {
		return err
	}

	_, err = dbClient.ProposeTransitions([]hdb.Transition{
		&node.ProcessRunningTransition{
			ProcessID: processID,
		},
	})
	if err != nil {
		return err
	}

	return nil
}

func (c *BaseNodeController) StopProcess(processID string) error {
	dbClient, err := c.databaseManager.GetDatabaseClientByName(constants.NodeDBDefaultName)
	if err != nil {
		return err
	}

	_, err = dbClient.ProposeTransitions([]hdb.Transition{
		&node.ProcessStopTransition{
			ProcessID: processID,
		},
	})
	if err != nil {
		return err
	}

	return nil
}

func (c *BaseNodeController) AddUser(userID, username, certificate string) error {
	dbClient, err := c.databaseManager.GetDatabaseClientByName(constants.NodeDBDefaultName)
	if err != nil {
		return err
	}

	_, err = dbClient.ProposeTransitions([]hdb.Transition{
		&node.AddUserTransition{
			UserID:      userID,
			Username:    username,
			Certificate: certificate,
		},
	})
	return err
}

func (c *BaseNodeController) GetUserByUsername(username string) (*node.User, error) {
	dbClient, err := c.databaseManager.GetDatabaseClientByName(constants.NodeDBDefaultName)
	if err != nil {
		return nil, err
	}

	var nodeState node.NodeState
	err = json.Unmarshal(dbClient.Bytes(), &nodeState)
	if err != nil {
		return nil, err
	}

	for _, user := range nodeState.Users {
		if user.Username == username {
			return user, err
		}
	}

	return nil, fmt.Errorf("user with username %s not found", username)
}
