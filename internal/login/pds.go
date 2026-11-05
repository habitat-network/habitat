package login

import (
	"context"
	"fmt"
	"net/url"

	"github.com/habitat-network/habitat/internal/pdsoauth"
)

// pdsProvider logs members in with their atproto PDS, through pear's OAuth
// client. The client keeps the pending authorization (PKCE verifier, DPoP key)
// in its store under the OAuth state and the session it ends up with, so the
// provider has no state of its own.
type pdsProvider struct {
	app *pdsoauth.App
}

func NewPDSProvider(app *pdsoauth.App) Provider {
	return &pdsProvider{app: app}
}

func (p *pdsProvider) Authorize(
	ctx context.Context,
	loginHint string,
) (string, string, string, error) {
	// TODO if login hint is empty (like when authing an org), we need to redirect to a page that let's the user
	// enter their handle so we can resolve the right pds to auth with.
	ctx, authState := pdsoauth.WithAuthState(ctx)
	redirect, err := p.app.StartAuthFlow(ctx, loginHint)
	if err != nil {
		return "", "", "", fmt.Errorf("start pds auth flow: %w", err)
	}
	return redirect, authState(), "", nil
}

func (p *pdsProvider) Exchange(
	ctx context.Context,
	query url.Values,
	_ string,
) (loginID string, profile Profile, err error) {
	// ProcessCallback consumes the pending authorization, so replaying the
	// callback fails, and stores the session.
	sess, err := p.app.ProcessCallback(ctx, query)
	if err != nil {
		return "", Profile{}, fmt.Errorf("process pds callback: %w", err)
	}
	return sess.AccountDID.String(), Profile{}, nil
}
