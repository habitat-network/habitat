package mcpoauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// ErrLoginDenied is returned by Broker.Finish when the user declined, or the
// PDS refused, the atproto login.
var ErrLoginDenied = errors.New("atproto login denied")

// Login is a completed atproto login.
type Login struct {
	// DID is the account the user proved control of.
	DID syntax.DID
	// State is the token Start returned for this login flow.
	State string
}

// Broker runs the atproto OAuth flow that proves which account the user is.
// The MCP authorization server delegates to it for the user's PDS, so an
// account hosted by pear and one hosted elsewhere go through the same code.
type Broker interface {
	// Start begins atproto OAuth for the account named by identifier (a handle
	// or DID). It returns the URL to send the user's browser to and the state
	// token that identifies the flow when the browser comes back.
	Start(ctx context.Context, identifier string) (redirectURL string, state string, err error)
	// Finish completes the flow from the callback's query parameters.
	Finish(ctx context.Context, query url.Values) (Login, error)
}

// indigoBroker is the Broker backed by indigo's atproto OAuth client.
type indigoBroker struct {
	app *oauth.ClientApp
}

// NewIndigoBroker returns a Broker using indigo's OAuth ClientApp. dir resolves
// handles and DIDs (including identities pear itself hosts), and client is used
// for all outbound requests.
func NewIndigoBroker(
	config oauth.ClientConfig,
	store oauth.ClientAuthStore,
	dir identity.Directory,
	client *http.Client,
) Broker {
	app := oauth.NewClientApp(&config, store)
	app.Dir = dir
	app.Client = client
	app.Resolver.Client = client
	return &indigoBroker{app: app}
}

// Start mirrors oauth.ClientApp.StartAuthFlow, but returns the flow's state
// token so the caller can associate its own pending request with it.
func (b *indigoBroker) Start(ctx context.Context, identifier string) (string, string, error) {
	atid, err := syntax.ParseAtIdentifier(identifier)
	if err != nil {
		return "", "", fmt.Errorf("not a valid account identifier: %w", err)
	}
	ident, err := b.app.Dir.Lookup(ctx, atid)
	if err != nil {
		return "", "", fmt.Errorf("resolve account: %w", err)
	}
	host := ident.PDSEndpoint()
	if host == "" {
		return "", "", errors.New("account does not link to an atproto host (PDS)")
	}
	authServerURL, err := b.app.Resolver.ResolveAuthServerURL(ctx, host)
	if err != nil {
		return "", "", fmt.Errorf("resolve auth server: %w", err)
	}
	meta, err := b.app.Resolver.ResolveAuthServerMetadata(ctx, authServerURL)
	if err != nil {
		return "", "", fmt.Errorf("fetch auth server metadata: %w", err)
	}
	info, err := b.app.SendAuthRequest(ctx, meta, b.app.Config.Scopes, identifier)
	if err != nil {
		return "", "", fmt.Errorf("auth request: %w", err)
	}
	info.AccountDID = &ident.DID
	if err := b.app.Store.SaveAuthRequestInfo(ctx, *info); err != nil {
		return "", "", fmt.Errorf("save auth request: %w", err)
	}

	params := url.Values{}
	params.Set("client_id", b.app.Config.ClientID)
	params.Set("request_uri", info.RequestURI)
	return meta.AuthorizationEndpoint + "?" + params.Encode(), info.State, nil
}

// Finish implements Broker.
func (b *indigoBroker) Finish(ctx context.Context, query url.Values) (Login, error) {
	sess, err := b.app.ProcessCallback(ctx, query)
	if err != nil {
		if _, ok := errors.AsType[*oauth.AuthRequestCallbackError](err); ok {
			return Login{}, fmt.Errorf("%w: %w", ErrLoginDenied, err)
		}
		return Login{}, err
	}
	return Login{DID: sess.AccountDID, State: sess.SessionID}, nil
}
