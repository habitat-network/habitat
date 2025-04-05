package controller

import (
	"context"
	"encoding/json"
	"fmt"

	types "github.com/eagraf/habitat-new/core/api"
	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/constants"
	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/rs/zerolog/log"
	"golang.org/x/mod/semver"
)

// implements nodeServerInner
type NodeController interface {
	InitializeNodeDB(ctx context.Context, transitions []hdb.Transition) error
	MigrateNodeDB(targetVersion string) error

	AddUser(userID, email, handle, password, certificate string) (types.PDSCreateAccountResponse, error)
	GetUserByUsername(username string) (*node.User, error)
}

type BaseNodeController struct {
	databaseManager hdb.HDBManager
	pdsClient       PDSClientI

	// TODO: eventually BaseNodeController will be fully replaced with Controller2 and renamed aptly.
	// However, until a final migration, there are still some state update subscribers that call the NodeController interface.
	// This will not be possible after the refactor but during the migration, mux these calls out with the new controller under
	// the hood.
	//
	// See StartProcess() for an example of this.
	ctrl2 *Controller2
}

func NewNodeController(habitatDBManager hdb.HDBManager, pds PDSClientI) (*BaseNodeController, error) {
	controller := &BaseNodeController{
		databaseManager: habitatDBManager,
		pdsClient:       pds,
	}
	return controller, nil
}

// TODO: Eventually BaseNodeController will be removed completely and replaced with Controller2,
// so not thinking about this too hard right now. But I had to add a mock call in 50+ callsites as an alternative
// to doing this, so it was the best path forward.
//
// As a consequence, any users of ctrl2 need to check if it is nil first.
func (c *BaseNodeController) SetCtrl2(c2 *Controller2) {
	c.ctrl2 = c2
}

// InitializeNodeDB tries initializing the database; it is a noop if a database with the same name already exists
func (c *BaseNodeController) InitializeNodeDB(ctx context.Context, transitions []hdb.Transition) error {
	_, err := c.databaseManager.CreateDatabase(ctx, constants.NodeDBDefaultName, node.SchemaName, transitions)
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
