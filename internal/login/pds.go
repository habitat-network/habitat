package login

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/org"
)

type pdsProvider struct {
	app *oauth.ClientApp
}

type pdsProviderState struct {
	OAuthState string `json:"oauth_state"`
}

func NewPDSProvider(app *oauth.ClientApp) Provider {
	return &pdsProvider{app: app}
}

func (p *pdsProvider) LoginMethod() org.LoginMethod { return org.LoginMethodAtproto }

func (p *pdsProvider) Authorize(
	ctx context.Context,
	id *identity.Identity,
	loginID string,
) (string, []byte, error) {
	if p.app == nil {
		return "", nil, fmt.Errorf("oauth client app not configured")
	}
	identifier := id.DID.String()
	if loginID != "" {
		publicDID, err := syntax.ParseDID(loginID)
		if err == nil {
			publicID, err := p.app.Dir.LookupDID(ctx, publicDID)
			if err == nil {
				id = publicID
			}
			identifier = publicDID.String()
		}
	}

	redirect, err := p.app.StartAuthFlow(ctx, identifier)
	if err != nil {
		return "", nil, fmt.Errorf("start auth flow: %w", err)
	}

	stateBytes, err := json.Marshal(pdsProviderState{OAuthState: ""})
	if err != nil {
		return "", nil, fmt.Errorf("marshal state: %w", err)
	}

	return redirect, stateBytes, nil
}

func (p *pdsProvider) Exchange(
	ctx context.Context,
	did syntax.DID,
	code string,
	issuer string,
	oauthState string,
	stateBytes []byte,
) error {
	if p.app == nil {
		return fmt.Errorf("oauth client app not configured")
	}
	params := url.Values{
		"code":  {code},
		"iss":   {issuer},
		"state": {oauthState},
	}
	_, err := p.app.ProcessCallback(ctx, params)
	if err != nil {
		var cbErr *oauth.AuthRequestCallbackError
		if errors.As(err, &cbErr) {
			return fmt.Errorf("auth callback error: %s: %s", cbErr.ErrorCode, cbErr.ErrorDescription)
		}
		return fmt.Errorf("process callback: %w", err)
	}
	return nil
}
