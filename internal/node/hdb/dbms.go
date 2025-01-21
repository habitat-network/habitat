package hdb

type HDBManager interface {
	Start()
	Stop()
	RestartDBs() error
	CreateDatabase(name, schemaType string, initialTransitions []Transition) (Client, error)
	GetDatabaseClientByName(name string) (Client, error)
}

type DatabaseConfig interface {
	ID() string
	Path() string
}
