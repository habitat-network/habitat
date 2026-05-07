package login

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

type habitatProvider struct {
	loginURL string
}

func NewHabitatProvider(frontendDomain string) Provider {
	return &habitatProvider{
		loginURL: fmt.Sprintf("https://%s/login/habitat", frontendDomain),
	}
}

func (h *habitatProvider) Type() ProviderType { return ProviderTypeHabitat }

func (h *habitatProvider) CanHandle(id *identity.Identity) bool {
	_, hasHabitat := id.Services["habitat"]
	_, hasPDS := id.Services["atproto_pds"]
	return hasHabitat && !hasPDS
}

/*
// Keep these here for reference for a sec
const (
	productionLogin = "https://habitat.network/habitat/#/login/habitat"
	devLogin        = "https://frontend.taile529e.ts.net/login/habitat"
)
*/

// Allow all logins to work for now, this is a work-in-progress
func (h *habitatProvider) Authorize(_ context.Context, _ *identity.Identity) (string, []byte, error) {
	return h.loginURL, []byte("placeholder"), nil // TODO: implement
}

func (h *habitatProvider) Exchange(_ context.Context, _ syntax.DID, _, _ string, _ []byte) error {
	return nil
}
