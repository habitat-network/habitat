package login

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/internal/pdscred"
)

type pdsProvider struct {
	oauthClient pdsclient.PdsOAuthClient
	credStore   pdscred.PDSCredentialStore
	dir         identity.Directory
}

// pdsProviderState is the opaque flash state for the PDS login flow.
type pdsProviderState struct {
	DpopKey        []byte                   `json:"dpop_key"`
	AuthorizeState pdsclient.AuthorizeState `json:"authorize_state"`
}

func NewPDSProvider(
	oauthClient pdsclient.PdsOAuthClient,
	credStore pdscred.PDSCredentialStore,
	dir identity.Directory,
) Provider {
	return &pdsProvider{oauthClient: oauthClient, credStore: credStore, dir: dir}
}

func (p *pdsProvider) Authorize(
	ctx context.Context,
	loginHint string,
) (string, []byte, error) {
	// TODO if login hint is empty (like when authing an org), we need to redirect to a page that let's the user
	// enter their handle so we can resolve the right pds to auth with.

	// If the member has a public ATProto DID as their loginID, resolve it and use
	// that identity's PDS for the OAuth flow. If no loginID (e.g. everyone org),
	// use the identity as-is.
	did, err := syntax.ParseDID(loginHint)
	if err != nil {
		return "", nil, fmt.Errorf("parse loginID: %w", err)
	}
	id, err := p.dir.LookupDID(ctx, did)
	if err != nil {
		return "", nil, fmt.Errorf("lookup loginID: %w", err)
	}

	dpopKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", nil, fmt.Errorf("generate dpop key: %w", err)
	}
	dpopClient := pdsclient.NewDpopHttpClient(dpopKey, &pdsclient.MemoryNonceProvider{})
	redirect, state, err := p.oauthClient.Authorize(ctx, dpopClient, id)
	if err != nil {
		return "", nil, err
	}
	dpopKeyBytes, err := dpopKey.Bytes()
	if err != nil {
		return "", nil, fmt.Errorf("serialize dpop key: %w", err)
	}
	stateBytes, err := json.Marshal(pdsProviderState{DpopKey: dpopKeyBytes, AuthorizeState: *state})
	if err != nil {
		return "", nil, fmt.Errorf("marshal pds provider state: %w", err)
	}
	return redirect, stateBytes, nil
}

func (p *pdsProvider) Exchange(
	ctx context.Context,
	code string,
	issuer string,
	stateBytes []byte,
) (loginID string, err error) {
	var s pdsProviderState
	if err := json.Unmarshal(stateBytes, &s); err != nil {
		return "", fmt.Errorf("unmarshal pds provider state: %w", err)
	}
	dpopKey, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), s.DpopKey)
	if err != nil {
		return "", fmt.Errorf("parse dpop key: %w", err)
	}
	dpopClient := pdsclient.NewDpopHttpClient(dpopKey, &pdsclient.MemoryNonceProvider{})
	tokenInfo, err := p.oauthClient.ExchangeCode(dpopClient, code, issuer, &s.AuthorizeState)
	if err != nil {
		return "", fmt.Errorf("exchange code: %w", err)
	}
	token, err := jwt.ParseSigned(tokenInfo.AccessToken)
	if err != nil {
		return "", fmt.Errorf("parse access token: %w", err)
	}
	var claims jwt.Claims
	if err := token.UnsafeClaimsWithoutVerification(&claims); err != nil {
		return "", fmt.Errorf("parse access token claims: %w", err)
	}
	loginDID, err := syntax.ParseDID(claims.Subject)
	if err != nil {
		return "", fmt.Errorf("parse loginID: %w", err)
	}
	if err := p.credStore.UpsertCredentials(ctx, loginDID, &pdscred.Credentials{
		AccessToken:  tokenInfo.AccessToken,
		RefreshToken: tokenInfo.RefreshToken,
		DpopKey:      dpopKey,
	}); err != nil {
		// Log and move on since an error upserting doesn't mean the login failed
		slog.WarnContext(ctx, "error upserting PDS credentials", "err", err)
	}
	return loginDID.String(), nil
}
