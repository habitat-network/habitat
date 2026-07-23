package pdsclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/encrypt"
	"gorm.io/gorm"
)

type PdsOAuthClient interface {
	ClientMetadata() *oauth.ClientMetadata
	Authorize(ctx context.Context, identifier string) (redirectUri string, err error)
	// ExchangeCode completes the OAuth callback, persists the session, and
	// returns the authenticated account DID. state is the OAuth "state" query
	// param echoed back by the auth server.
	ExchangeCode(ctx context.Context, code, issuer, state string) (syntax.DID, error)
	Do(ctx context.Context, did syntax.DID, req *http.Request) (*http.Response, error)
}

type pdsClientImpl struct {
	app *oauth.ClientApp
}

func NewClient(
	db *gorm.DB,
	clientId string,
	clientUri string,
	redirectUri string,
	secret string,
) (PdsOAuthClient, error) {
	config := oauth.NewPublicConfig(
		clientId,
		redirectUri,
		[]string{"atproto", "transition:generic"},
	)
	clientSecret, err := encrypt.ParseKey(secret)
	if err != nil {
		return nil, err
	}
	clientKey, err := atcrypto.ParsePrivateBytesP256(clientSecret)
	if err != nil {
		return nil, err
	}
	if err := config.SetClientSecret(clientKey, "habitat"); err != nil {
		return nil, fmt.Errorf("set client secret: %w", err)
	}

	store, err := NewOAuthStore(db, clientSecret)
	if err != nil {
		return nil, fmt.Errorf("new oauth store: %w", err)
	}

	app := oauth.NewClientApp(&config, store)

	return &pdsClientImpl{app: app}, nil
}

// Authorize implements [PdsOAuthClient].
func (p *pdsClientImpl) Authorize(
	ctx context.Context,
	identifier string,
) (redirectUri string, err error) {
	return p.app.StartAuthFlow(ctx, identifier)
}

// ClientMetadata implements [PdsOAuthClient].
func (p *pdsClientImpl) ClientMetadata() *oauth.ClientMetadata {
	metadata := p.app.Config.ClientMetadata()
	name := "Habitat"
	metadata.ClientName = &name
	return &metadata
}

// Do implements [PdsOAuthClient].
func (p *pdsClientImpl) Do(
	ctx context.Context,
	did syntax.DID,
	req *http.Request,
) (*http.Response, error) {
	session, err := p.app.ResumeSession(ctx, did, DefaultSessionID)
	if err != nil {
		return nil, fmt.Errorf("resume session: %w", err)
	}
	if req.URL.Host == "" {
		hostURL, err := url.Parse(session.Data.HostURL)
		if err != nil {
			return nil, fmt.Errorf("parse host url: %w", err)
		}
		req.URL = hostURL.ResolveReference(req.URL)
	}
	nsidStr := strings.TrimPrefix(req.URL.Path, "/xrpc/")
	nsid, err := syntax.ParseNSID(nsidStr)
	if err != nil {
		return nil, fmt.Errorf("parse nsid: %w", err)
	}
	return session.DoWithAuth(http.DefaultClient, req, nsid)
}

// ExchangeCode implements [PdsOAuthClient].
func (p *pdsClientImpl) ExchangeCode(
	ctx context.Context,
	code string,
	issuer string,
	state string,
) (syntax.DID, error) {
	params := url.Values{
		"code":  {code},
		"iss":   {issuer},
		"state": {state},
	}
	sess, err := p.app.ProcessCallback(ctx, params)
	if err != nil {
		return "", fmt.Errorf("process callback: %w", err)
	}
	return sess.AccountDID, nil
}
