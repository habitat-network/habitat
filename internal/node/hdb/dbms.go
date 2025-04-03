package hdb

import "context"

type HDBManager interface {
	Start(context.Context)
	Stop()
	RestartDBs(context.Context) error
	CreateDatabase(ctx context.Context, name, schemaType string, initialTransitions []Transition) (Client, error)
	GetDatabaseClientByName(name string) (Client, error)
}

type DatabaseConfig interface {
	ID() string
	Path() string
}
