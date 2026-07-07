package oauthclient

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
	"golang.org/x/sync/singleflight"
)

type App struct {
	Config *oauth.ClientConfig
	dir    identity.Directory
	store  oauth.ClientAuthStore

	refreshG singleflight.Group
}

type Option func(*App)

func WithDirectory(dir identity.Directory) Option {
	return func(a *App) {
		a.dir = dir
	}
}

func NewApp(config *oauth.ClientConfig, store oauth.ClientAuthStore, opts ...Option) *App {
	app := &App{
		Config: config,
		dir:    identity.DefaultDirectory(),
		store:  store,
	}
	for _, opt := range opts {
		opt(app)
	}
	return app
}

func (a *App) StartAuthFlow(ctx context.Context, identifier string) (string, error) {
	atid, err := syntax.ParseAtIdentifier(identifier)
	if err != nil {
		return "", fmt.Errorf("parse identifier: %w", err)
	}

	id, err := a.dir.Lookup(ctx, atid)
	if err != nil {
		return "", fmt.Errorf("lookup identity: %w", err)
	}

	pdsHost, ok := id.Services["habitat"]
	if !ok || pdsHost.URL == "" {
		return "", fmt.Errorf("no Habitat endpoint for %q", identifier)
	}

	verifier := oauth2.GenerateVerifier()
	b := make([]byte, 16)
	rand.Read(b)
	state := base64.URLEncoding.EncodeToString(b)

	info := oauth.AuthRequestData{
		State:                   state,
		PKCEVerifier:            verifier,
		AuthServerURL:           pdsHost.URL,
		AuthServerTokenEndpoint: pdsHost.URL + "/oauth/token",
		Scopes:                  a.Config.Scopes,
		AccountDID:              &id.DID,
	}

	if err := a.store.SaveAuthRequestInfo(ctx, info); err != nil {
		return "", fmt.Errorf("save auth request info: %w", err)
	}

	return internalConfig(a.Config, pdsHost.URL).AuthCodeURL(
		state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("handle", identifier),
	), nil
}

func (a *App) ProcessCallback(
	ctx context.Context,
	params url.Values,
) (*oauth.ClientSessionData, error) {
	state := params.Get("state")
	code := params.Get("code")
	if state == "" || code == "" {
		return nil, fmt.Errorf("missing state or code")
	}

	info, err := a.store.GetAuthRequestInfo(ctx, state)
	if err != nil {
		return nil, fmt.Errorf("load auth request info: %w", err)
	}

	if info.AccountDID == nil {
		return nil, fmt.Errorf("auth request info missing account DID")
	}
	accountDID := *info.AccountDID

	oauthToken, err := internalConfig(a.Config, info.AuthServerURL).Exchange(
		ctx, code, oauth2.VerifierOption(info.PKCEVerifier),
	)
	if err != nil {
		return nil, fmt.Errorf("exchange code: %w", err)
	}

	ident, err := a.dir.LookupDID(ctx, accountDID)
	if err != nil {
		return nil, fmt.Errorf("lookup DID: %w", err)
	}
	hostURL := ident.Services["habitat"].URL

	sessData := oauth.ClientSessionData{
		AccountDID:              accountDID,
		SessionID:               state,
		HostURL:                 hostURL,
		AuthServerURL:           info.AuthServerURL,
		AuthServerTokenEndpoint: info.AuthServerTokenEndpoint,
		Scopes:                  info.Scopes,
		AccessToken:             oauthToken.AccessToken,
		RefreshToken:            oauthToken.RefreshToken,
	}

	if err := a.store.SaveSession(ctx, sessData); err != nil {
		return nil, fmt.Errorf("save session: %w", err)
	}
	if err := a.store.DeleteAuthRequestInfo(ctx, state); err != nil {
		return nil, fmt.Errorf("delete auth request info: %w", err)
	}

	return &sessData, nil
}

func (a *App) AddSessionWithBearerJwt(
	ctx context.Context,
	did syntax.DID,
) (*oauth.ClientSessionData, error) {
	id, err := a.dir.LookupDID(ctx, did)
	if err != nil {
		return nil, fmt.Errorf("lookup DID: %w", err)
	}
	pearURL := id.GetServiceEndpoint("habitat")

	jwt, err := jwt.NewWithClaims(signingMethodES256, jwt.RegisteredClaims{
		Issuer:    a.Config.ClientID,
		Audience:  []string{pearURL + "/oauth/token"},
		Subject:   did.String(),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute * 5)),
		ID:        uuid.NewString(),
	}).SignedString(a.Config.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("create client assertion: %w", err)
	}

	resp, err := http.Post(
		pearURL+"/oauth/token",
		"application/x-www-form-urlencoded",
		strings.NewReader(url.Values{
			"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
			"assertion":  {jwt},
		}.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("post token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("post token: %s", resp.Status)
	}

	var token oauth2.Token
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("decode token: %w", err)
	}
	sessData := oauth.ClientSessionData{
		AccountDID:              did,
		SessionID:               uuid.NewString(),
		HostURL:                 pearURL,
		AuthServerURL:           pearURL,
		AuthServerTokenEndpoint: pearURL + "/oauth/token",
		Scopes:                  a.Config.Scopes,
		AccessToken:             token.AccessToken,
		RefreshToken:            token.RefreshToken,
	}
	if err := a.store.SaveSession(ctx, sessData); err != nil {
		return nil, fmt.Errorf("save session: %w", err)
	}
	return &sessData, nil
}

func (a *App) GetClient(
	ctx context.Context,
	did syntax.DID,
	sessionID string,
) (*http.Client, error) {
	return NewClient(ctx, a.store, a.Config, did, sessionID, &a.refreshG)
}

func (a *App) Logout(ctx context.Context, did syntax.DID, sessionID string) error {
	return a.store.DeleteSession(ctx, did, sessionID)
}

func internalConfig(config *oauth.ClientConfig, hostURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:    config.ClientID,
		RedirectURL: config.CallbackURL,
		Scopes:      config.Scopes,
		Endpoint: oauth2.Endpoint{
			TokenURL:  hostURL + "/oauth/token",
			AuthURL:   hostURL + "/oauth/authorize",
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}
}
