package login

import (
	"context"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

type habitatProvider struct{}

func NewHabitatProvider() Provider { return &habitatProvider{} }

func (h *habitatProvider) Type() ProviderType { return ProviderTypeHabitat }

func (h *habitatProvider) CanHandle(id *identity.Identity) bool {
	_, hasHabitat := id.Services["habitat"]
	_, hasPDS := id.Services["atproto_pds"]
	return hasHabitat && !hasPDS
}

// Allow all logins to work for now, this is a work-in-progress
func (h *habitatProvider) Authorize(_ context.Context, _ *identity.Identity) (string, []byte, error) {
	return "https://habitat.network/habitat/#/login/habitat", []byte("placeholder"), nil // TODO: to work in dev this should really be https://[frontend_domain]/login/habitat
}

func (h *habitatProvider) Exchange(_ context.Context, _ syntax.DID, _, _ string, _ []byte) error {
	return nil
}
