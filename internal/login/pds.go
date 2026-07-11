package login

import (
	"context"
	"fmt"

	"github.com/habitat-network/habitat/internal/pdsclient"
)

// pdsProvider logs members in against their ATProto PDS using an OAuth client.
// Session credentials are persisted by the underlying pdsclient (indigo OAuth
// client store), so this provider is a thin adapter onto the login.Provider
// interface.
type pdsProvider struct {
	client pdsclient.PdsOAuthClient
}

func NewPDSProvider(client pdsclient.PdsOAuthClient) Provider {
	return &pdsProvider{client: client}
}

func (p *pdsProvider) Authorize(
	ctx context.Context,
	loginHint string,
) (string, []byte, error) {
	if p.client == nil {
		return "", nil, fmt.Errorf("oauth client not configured")
	}
	// loginHint is the member's LoginID (a DID or handle). The OAuth client
	// resolves it to the right PDS and starts the auth flow. An empty hint
	// (e.g. authing an org where any admin will do) can't be resolved to a PDS.
	// TODO: redirect empty hints to a page that collects the user's handle.
	if loginHint == "" {
		return "", nil, fmt.Errorf("atproto login requires a handle")
	}

	redirect, err := p.client.Authorize(ctx, loginHint)
	if err != nil {
		return "", nil, fmt.Errorf("start auth flow: %w", err)
	}
	// The OAuth client persists the pending auth request keyed by the OAuth
	// state param, so no provider-specific flash state is needed.
	return redirect, nil, nil
}

func (p *pdsProvider) Exchange(
	ctx context.Context,
	code string,
	issuer string,
	oauthState string,
	_ []byte,
) (loginID string, err error) {
	if p.client == nil {
		return "", fmt.Errorf("oauth client not configured")
	}
	did, err := p.client.ExchangeCode(ctx, code, issuer, oauthState)
	if err != nil {
		return "", fmt.Errorf("exchange code: %w", err)
	}
	return did.String(), nil
}
