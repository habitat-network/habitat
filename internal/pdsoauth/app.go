package pdsoauth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/httpx"
)

const (
	clientName = "Habitat"
	// clientKeyID names the client assertion key in the published JWKS.
	clientKeyID = "habitat"
)

// Scopes are what pear asks of a member's PDS.
var Scopes = []string{"atproto", "transition:generic"}

// Config configures pear's atproto OAuth client.
type Config struct {
	// ClientID is the URL of the client metadata document.
	ClientID string
	// ClientURI is the client's homepage.
	ClientURI string
	// CallbackURL is where PDSes redirect members after authorizing.
	CallbackURL string
	// Secret is the encoded P-256 client assertion key (see encrypt.ParseKey).
	Secret string
}

// App is pear's atproto OAuth client.
type App struct {
	*oauth.ClientApp
	clientURI string
}

// NewApp returns the OAuth client app, resolving identities through dir.
func NewApp(cfg Config, store oauth.ClientAuthStore, dir identity.Directory) (*App, error) {
	secretBytes, err := encrypt.ParseKey(cfg.Secret)
	if err != nil {
		return nil, fmt.Errorf("parse client secret: %w", err)
	}
	ecKey, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), secretBytes)
	if err != nil {
		return nil, fmt.Errorf("parse client key: %w", err)
	}
	// ecdsa.PrivateKey.Bytes is the same fixed-size scalar atcrypto parses.
	scalar, err := ecKey.Bytes()
	if err != nil {
		return nil, fmt.Errorf("encode client key: %w", err)
	}
	priv, err := atcrypto.ParsePrivateBytesP256(scalar)
	if err != nil {
		return nil, fmt.Errorf("load client key: %w", err)
	}
	config := oauth.NewPublicConfig(cfg.ClientID, cfg.CallbackURL, Scopes)
	if err := config.SetClientSecret(priv, clientKeyID); err != nil {
		return nil, fmt.Errorf("set client secret: %w", err)
	}
	app := oauth.NewClientApp(&config, store)
	app.Client = httpx.NewClient()
	app.Resolver.Client = httpx.NewClient()
	app.Dir = dir
	return &App{ClientApp: app, clientURI: cfg.ClientURI}, nil
}

// ClientMetadata is the document served at the client ID URL.
func (a *App) ClientMetadata() oauth.ClientMetadata {
	m := a.Config.ClientMetadata()
	jwks := a.Config.PublicJWKS()
	m.JWKS = &jwks
	name := clientName
	m.ClientName = &name
	m.ClientURI = &a.clientURI
	return m
}

type authStateKey struct{}

// WithAuthState returns a context for StartAuthFlow along with a function
// that, after the flow has started, gives the OAuth state the authorization
// callback will echo back. indigo keeps the state private to its store, so
// the store reports it through the context.
func WithAuthState(ctx context.Context) (context.Context, func() string) {
	var state string
	ctx = context.WithValue(ctx, authStateKey{}, &state)
	return ctx, func() string { return state }
}

func reportAuthState(ctx context.Context, state string) {
	if p, ok := ctx.Value(authStateKey{}).(*string); ok {
		*p = state
	}
}
