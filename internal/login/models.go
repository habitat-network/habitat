package login

// Models returns the GORM models this package persists. Their tables are
// created by db.Migrate.
func Models() []any {
	return []any{&googleCredentialsModel{}, &passwordEntry{}}
}
