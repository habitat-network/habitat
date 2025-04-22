package controller

import (
	"context"
	"encoding/json"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/constants"
	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/rs/zerolog/log"
	"golang.org/x/mod/semver"
)

// InitializeNodeDB tries initializing the database; it is a noop if a database with the same name already exists
func InitializeNodeDB(ctx context.Context, databaseManager hdb.HDBManager, transitions []hdb.Transition) error {
	_, err := databaseManager.CreateDatabase(ctx, constants.NodeDBDefaultName, node.SchemaName, transitions)
	if err != nil {
		if _, ok := err.(*hdb.DatabaseAlreadyExistsError); ok {
			log.Info().Msg("Node database already exists, doing nothing.")
		} else {
			return err
		}
	}
	return nil
}

func MigrateNodeDB(databaseManager hdb.HDBManager, targetVersion string) error {
	dbClient, err := databaseManager.GetDatabaseClientByName(constants.NodeDBDefaultName)
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
		node.CreateMigrationTransition(targetVersion),
	})
	return err
}
