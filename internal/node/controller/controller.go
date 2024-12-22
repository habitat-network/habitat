package controller

import (
	"encoding/json"
	"fmt"

	types "github.com/eagraf/habitat-new/core/api"
	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/constants"
	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/rs/zerolog/log"
	"golang.org/x/mod/semver"
)

// NodeController is an interface to manage common admin actions on a Habitat node.
// For example, installing apps or adding users. This will likely expand to be a much bigger API as we move forward.

type NodeController interface {
	InitializeNodeDB(transitions []hdb.Transition) error
	MigrateNodeDB(targetVersion string) error

	AddUser(userID, email, handle, password, certificate string) (types.PDSCreateAccountResponse, error)
	GetUserByUsername(username string) (*node.User, error)

	InstallApp(userID string, newApp *node.AppInstallation, newProxyRules []*node.ReverseProxyRule) error
	FinishAppInstallation(userID string, appID, registryURLBase, registryPackageID string, startAfterInstall bool) error
	GetAppByID(appID string) (*node.AppInstallation, error)

	StartProcess(appID string) error
	SetProcessRunning(processID string) error
	StopProcess(processID string) error
}

type BaseNodeController struct {
	databaseManager hdb.HDBManager
	pdsClient       PDSClientI
}

func NewNodeController(habitatDBManager hdb.HDBManager, pds PDSClientI) (*BaseNodeController, error) {
	controller := &BaseNodeController{
		databaseManager: habitatDBManager,
		pdsClient:       pds,
	}
	return controller, nil
}

// InitializeNodeDB tries initializing the database; it is a noop if a database with the same name already exists
func (c *BaseNodeController) InitializeNodeDB(transitions []hdb.Transition) error {
	_, err := c.databaseManager.CreateDatabase(constants.NodeDBDefaultName, node.SchemaName, transitions)
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

	var nodeState node.State
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
			UserID:                 userID,
			AppInstallation:        newApp,
			NewProxyRules:          newProxyRules,
			StartAfterInstallation: true,
		},
	})
	return err
}

// FinishAppInstallation marks the app lifecycle state as installed
func (c *BaseNodeController) FinishAppInstallation(userID string, appID, registryURLBase, registryAppID string, startAfterInstall bool) error {
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

			StartAfterInstallation: startAfterInstall,
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

	var nodeState node.State
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

func (c *BaseNodeController) StartProcess(appID string) error {
	dbClient, err := c.databaseManager.GetDatabaseClientByName(constants.NodeDBDefaultName)
	if err != nil {
		return err
	}

	var nodeState node.State
	err = json.Unmarshal(dbClient.Bytes(), &nodeState)
	if err != nil {
		return nil
	}

	_, err = dbClient.ProposeTransitions([]hdb.Transition{
		&node.ProcessStartTransition{
			AppID: appID,
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

func (c *BaseNodeController) AddUser(userID, email, handle, password, certificate string) (types.PDSCreateAccountResponse, error) {
	resp, err := c.pdsClient.CreateAccount(email, handle, password)
	if err != nil {
		return nil, err
	}
	userDID := ""
	did, ok := resp["did"]
	if ok {
		if did == nil {
			return nil, fmt.Errorf("PDS response did not contain a DID (nil)")
		}

		userDID = did.(string)
	}

	if !ok || userDID == "" {
		return nil, fmt.Errorf("PDS response did not contain a DID")
	}

	dbClient, err := c.databaseManager.GetDatabaseClientByName(constants.NodeDBDefaultName)
	if err != nil {
		return nil, err
	}

	_, err = dbClient.ProposeTransitions([]hdb.Transition{
		&node.AddUserTransition{
			Username:    handle,
			Certificate: certificate,
			AtprotoDID:  userDID,
		},
	})
	return resp, err
}

func (c *BaseNodeController) GetUserByUsername(username string) (*node.User, error) {
	dbClient, err := c.databaseManager.GetDatabaseClientByName(constants.NodeDBDefaultName)
	if err != nil {
		return nil, err
	}

	var nodeState node.State
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
