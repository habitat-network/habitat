package types

type GetDatabaseResponse struct {
	DatabaseID string                   `json:"database_id"`
	State      map[string]interface{}   `json:"state"`
	Dex        []map[string]interface{} `json:"dex,omitempty"`
}
