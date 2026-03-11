package server

// Serialize route names for reporting (see metrics)
const (
	PutRecord        = "put_record"
	GetRecord        = "get_record"
	ListRecords      = "list_records"
	GetBlob          = "get_blob"
	UploadBlob       = "upload_blob"
	AddPermission    = "add_permission"
	RemovePermission = "remove_permission"
	ListPermissions  = "list_permissions"
	ListCollections  = "list_collections"
	NotifyOfUpdate   = "notify_of_update"
)

var (
	Routes = []string{
		PutRecord,
		GetRecord,
		ListRecords,
		GetBlob,
		UploadBlob,
		AddPermission,
		RemovePermission,
		ListPermissions,
		ListCollections,
		NotifyOfUpdate,
	}
)
