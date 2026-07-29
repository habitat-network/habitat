package login

import (
	"context"
	"fmt"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
)

// pdsProvider logs members in against their ATProto PDS using an OAuth
// client. Session credentials are persisted by the client's store, so this
// provider is a thin adapter onto the login.Provider interface.
type pdsProvider struct {
	app *oauth.ClientApp
}

func NewPDSProvider(app *oauth.ClientApp) Provider {
	return &pdsProvider{app: app}
}

func (p *pdsProvider) Authorize(
	ctx context.Context,
	loginHint string,
) (string, []byte, error) {
	// loginHint is the member's LoginID (a DID or handle). The OAuth client
	// resolves it to the right PDS and starts the auth flow. An empty hint
	// (e.g. authing an org where any admin will do) can't be resolved to a PDS.
	// TODO: redirect empty hints to a page that collects the user's handle.
	if loginHint == "" {
		return "", nil, fmt.Errorf("atproto login requires a handle")
	}

	redirect, err := p.app.StartAuthFlow(ctx, loginHint)
	if err != nil {
		return "", nil, fmt.Errorf("start auth flow: %w", err)
	}
	// The OAuth client persists the pending auth request keyed by the OAuth
	// state param, so no provider-specific flash state is needed.
	return redirect, nil, nil
}

func (p *pdsProvider) Exchange(
	ctx context.Context,
	query url.Values,
	_ []byte,
) (loginID string, err error) {
	sess, err := p.app.ProcessCallback(ctx, query)
	if err != nil {
		return "", fmt.Errorf("exchange code: %w", err)
	}
	return sess.AccountDID.String(), nil
}
