package types

type PDSCreateAccountRequest struct {
	Email    string `json:"email"`
	Handle   string `json:"handle"`
	Password string `json:"password"`
}

type PDSCreateAccountResponse map[string]interface{}

type PDSCreateSessionRequest struct {
	Identifier string `json:"identifier"` // email or handle
	Password   string `json:"password"`
}

type PDSCreateSessionResponse map[string]interface{}
