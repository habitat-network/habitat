package login

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/internal/pdscred"
	"log/slog"
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
	_ syntax.DID,
	loginID string,
) (string, []byte, error) {
	// If the member has a public ATProto DID as their loginID, resolve it and use
	// that identity's PDS for the OAuth flow. If no loginID (e.g. everyone org),
	// use the identity as-is.
	did, err := syntax.ParseDID(loginID)
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
	_ syntax.DID,
	loginId string,
	code string,
	issuer string,
	stateBytes []byte,
) error {
	var s pdsProviderState
	if err := json.Unmarshal(stateBytes, &s); err != nil {
		return fmt.Errorf("unmarshal pds provider state: %w", err)
	}
	dpopKey, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), s.DpopKey)
	if err != nil {
		return fmt.Errorf("parse dpop key: %w", err)
	}
	dpopClient := pdsclient.NewDpopHttpClient(dpopKey, &pdsclient.MemoryNonceProvider{})
	tokenInfo, err := p.oauthClient.ExchangeCode(dpopClient, code, issuer, &s.AuthorizeState)
	if err != nil {
		return fmt.Errorf("exchange code: %w", err)
	}
	if err := p.credStore.UpsertCredentials(ctx, syntax.DID(loginId), &pdscred.Credentials{
		AccessToken:  tokenInfo.AccessToken,
		RefreshToken: tokenInfo.RefreshToken,
		DpopKey:      dpopKey,
	}); err != nil {
		// Log and move on since an error upserting doesn't mean the login failed
		slog.WarnContext(ctx, "error upserting PDS credentials", "err", err)
	}
	return nil
}
