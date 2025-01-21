package node

// Types related to running processes, mostly used by internal/process
type Process struct {
	ID      string `json:"id"`
	AppID   string `json:"app_id"`
	UserID  string `json:"user_id"`
	Created string `json:"created"`
	Driver  string `json:"driver"`
}
