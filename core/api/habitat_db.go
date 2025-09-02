package types

type GetDatabaseResponse struct {
	State map[string]interface{}   `json:"state"`
	Dex   []map[string]interface{} `json:"dex,omitempty"`
}
