package hdbms

import (
	"context"

	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/eagraf/habitat-new/internal/pubsub"
	"github.com/rs/zerolog"
)

type HDBResult struct {
	Manager              hdb.HDBManager
	StateUpdatePublisher pubsub.Publisher[hdb.StateUpdate] `group:"state_update_publishers"`
}

func NewHabitatDB(ctx context.Context, logger *zerolog.Logger, publisher *pubsub.SimplePublisher[hdb.StateUpdate], path string) (*HDBResult, func(), error) {
	dbManager, err := NewDatabaseManager(path, publisher)
	if err != nil {
		return nil, nil, err
	}

	err = dbManager.RestartDBs(ctx)
	if err != nil {
		return nil, nil, err
	}

	dbManager.Start(ctx)

	return &HDBResult{
		Manager:              dbManager,
		StateUpdatePublisher: publisher,
	}, dbManager.Stop, nil
}

// StateUpdateLogger is a subscriber for StateUpdates that logs them.
type StateUpdateLogger struct {
	logger *zerolog.Logger
}

func (s *StateUpdateLogger) Name() string {
	return "StateUpdateLogger"
}

func (s *StateUpdateLogger) ConsumeEvent(event hdb.StateUpdate) error {
	if event.IsRestore() {
		s.logger.Info().Msgf("Restoring state for %s", event.DatabaseID())
		return nil
	} else {
		s.logger.Info().Msgf("Applying transition %s to %s", string(event.TransitionType()), event.DatabaseID())
		return nil
	}
}

func NewStateUpdateLogger(logger *zerolog.Logger) *StateUpdateLogger {
	return &StateUpdateLogger{
		logger: logger,
	}
}
