package login

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/pdsclient"
)

type pdsProvider struct {
	client pdsclient.PdsOAuthClient
	dir    identity.Directory
}

type pdsProviderState struct {
	OAuthState string `json:"oauth_state"`
}

func NewPDSProvider(client pdsclient.PdsOAuthClient, dir identity.Directory) Provider {
	return &pdsProvider{client: client, dir: dir}
}

func (p *pdsProvider) LoginMethod() org.LoginMethod { return org.LoginMethodAtproto }

func (p *pdsProvider) Authorize(
	ctx context.Context,
	id *identity.Identity,
	loginID string,
) (string, []byte, error) {
	if p.client == nil {
		return "", nil, fmt.Errorf("oauth client not configured")
	}
	identifier := id.DID.String()
	if loginID != "" {
		publicDID, err := syntax.ParseDID(loginID)
		if err == nil {
			publicID, err := p.dir.LookupDID(ctx, publicDID)
			if err == nil {
				id = publicID
			}
			identifier = publicDID.String()
		}
	}

	redirect, err := p.client.Authorize(ctx, identifier)
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
	if p.client == nil {
		return fmt.Errorf("oauth client not configured")
	}
	if err := p.client.ExchangeCode(ctx, code, issuer, oauthState); err != nil {
		return fmt.Errorf("exchange code: %w", err)
	}
	return nil
}
