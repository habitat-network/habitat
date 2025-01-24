package node

type ProcessID string

// Types related to running processes, mostly used by internal/process
type Process struct {
	ID      ProcessID `json:"id"`
	AppID   string    `json:"app_id"`
	UserID  string    `json:"user_id"`
	Created string    `json:"created"`
	Driver  string    `json:"driver"`
}
