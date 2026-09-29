package login

// Models exposes this package's GORM models to db.AutoMigrate, which creates
// their tables.
var Models = modeler{}

// modeler implements db.Modeler for this package.
type modeler struct{}

// Models returns the GORM models this package persists.
func (modeler) Models() []any {
	return []any{&googleCredentialsModel{}, &passwordEntry{}}
}
