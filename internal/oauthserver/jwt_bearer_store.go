package oauthserver

import "slices"

type ApprovedClientStore interface {
	IsApprovedClient(clientID string) bool
}

type jwtBearerStore struct {
	clients []string
}

func NewJWTBearerStore(builtinClients ...string) ApprovedClientStore {
	return &jwtBearerStore{clients: builtinClients}
}

func (s *jwtBearerStore) IsApprovedClient(clientID string) bool {
	return slices.Contains(s.clients, clientID)
}
