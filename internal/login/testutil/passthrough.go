package testutil

import (
	"context"

	"github.com/habitat-network/habitat/internal/login"
)

// PassthroughProvider implements [login.Provider] by immediately returning a
// fixed redirect URI on Authorize and accepting any code on Exchange. Useful
// for tests that want to skip the real PDS OAuth dance.
type PassthroughProvider struct {
	RedirectURI string
	LoginID     string
}

func NewPassthroughProvider(redirectURI string, loginID string) *PassthroughProvider {
	return &PassthroughProvider{
		RedirectURI: redirectURI,
		LoginID:     loginID,
	}
}

func (p *PassthroughProvider) Authorize(
	ctx context.Context,
	loginHint string,
) (redirectURI string, state []byte, err error) {
	return p.RedirectURI, nil, nil
}

func (p *PassthroughProvider) Exchange(
	ctx context.Context,
	code string,
	issuer string,
	state []byte,
) (loginID string, err error) {
	return p.LoginID, nil
}

var _ login.Provider = (*PassthroughProvider)(nil)
