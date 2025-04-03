package types

import node "github.com/eagraf/habitat-new/core/state/node"

type MigrateRequest struct {
	TargetVersion string `json:"target_version"`
}

type GetNodeResponse struct {
	State node.State `json:"state"`
}

type PostAddUserRequest struct {
	UserID      string `json:"user_id"`
	Handle      string `json:"handle"`
	Password    string `json:"password"`
	Email       string `json:"email"`
	Certificate string `json:"certificate"`
}

type PostAddUserResponse struct {
	PDSCreateAccountResponse map[string]interface{} `json:"pds_create_account_response"`
}
