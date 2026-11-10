package mcpgateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"golang.org/x/oauth2"
	"golang.org/x/sync/singleflight"

	"github.com/habitat-network/habitat/internal/keyval"
)

// TokensNamespace is the keyval namespace each member's tokens for an org's
// MCP servers are stored under, keyed by grant.key.
const TokensNamespace = "mcp_oauth_tokens"

// expirySkew is how long before its expiry an access token is refreshed, so
// it doesn't expire in flight.
const expirySkew = time.Minute

// refreshTimeout bounds a token refresh, which outlives the request that
// started it (see tokenVault.refresh).
const refreshTimeout = 30 * time.Second

// grant names a member's tokens for an org's server.
type grant struct {
	org    syntax.DID
	server ServerName
	// generation is the serverRow.Generation the tokens are for. Tokens
	// stored for another generation, of a server since removed and re-added
	// under the same name, aren't the grant's.
	generation string
	did        syntax.DID
}

// key keys g's tokens. Org DIDs are globally unique and a server's name is
// unique within its org, so the triple is too.
func (g grant) key() string {
	return g.org.String() + "|" + string(g.server) + "|" + g.did.String()
}

// storedToken is a member's tokens for a server, as stored in keyval.
type storedToken struct {
	// Generation is the serverRow.Generation the tokens were issued for.
	Generation   string
	AccessToken  string
	TokenType    string
	RefreshToken string
	Expiry       time.Time
}

func newStoredToken(generation string, token *oauth2.Token) *storedToken {
	return &storedToken{
		Generation:   generation,
		AccessToken:  token.AccessToken,
		TokenType:    token.TokenType,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
	}
}

// needsRefresh reports whether t's access token expires within expirySkew.
// Tokens without an expiry, such as personal access tokens, never do.
func (t *storedToken) needsRefresh() bool {
	return !t.Expiry.IsZero() && time.Until(t.Expiry) < expirySkew
}

func (t *storedToken) oauth2() *oauth2.Token {
	return &oauth2.Token{
		AccessToken:  t.AccessToken,
		TokenType:    t.TokenType,
		RefreshToken: t.RefreshToken,
		Expiry:       t.Expiry,
	}
}

// tokenVault keeps each member's tokens for their org's MCP servers,
// encrypted, and refreshes the ones that expire.
type tokenVault struct {
	tokens keyval.EncryptedStore
	// client makes the refresh requests.
	client *http.Client
	// refreshes collapses concurrent refreshes of the same tokens within this
	// process, since a rotating refresh token can only be redeemed once.
	refreshes singleflight.Group
}

func newTokenVault(encKV keyval.EncryptedStore, client *http.Client) *tokenVault {
	return &tokenVault{tokens: encKV.Namespace(TokensNamespace), client: client}
}

// get returns g's tokens, or ErrNotConnected if there are none.
func (v *tokenVault) get(ctx context.Context, g grant) (*storedToken, error) {
	var t storedToken
	err := v.tokens.GetEncrypted(ctx, g.key(), &t)
	if errors.Is(err, keyval.ErrNotFound) {
		return nil, ErrNotConnected
	} else if err != nil {
		return nil, fmt.Errorf("get mcp server tokens: %w", err)
	}
	if t.Generation != g.generation {
		return nil, ErrNotConnected
	}
	return &t, nil
}

// put stores token as g's tokens, replacing any there.
func (v *tokenVault) put(ctx context.Context, g grant, token *oauth2.Token) error {
	if err := v.tokens.PutEncrypted(
		ctx, g.key(), newStoredToken(g.generation, token), keyval.NoExpiry,
	); err != nil {
		return fmt.Errorf("store mcp server tokens: %w", err)
	}
	return nil
}

// delete deletes g's tokens, whatever generation they're for. It's no error
// if there are none.
func (v *tokenVault) delete(ctx context.Context, g grant) error {
	err := v.tokens.Delete(ctx, g.key())
	if err != nil && !errors.Is(err, keyval.ErrNotFound) {
		return fmt.Errorf("delete mcp server tokens: %w", err)
	}
	return nil
}

// refresh redeems g's refresh token with cfg's authorization server for new
// tokens and stores them, before any caller uses them: a rotated refresh
// token that isn't stored would leave the member disconnected at the next
// refresh. If the authorization server no longer honors the refresh token,
// g's tokens are deleted and refresh returns ErrNotConnected. Tokens that
// another caller refreshed meanwhile are returned as they are.
//
// Concurrent refreshes within this process share one request. Refreshes on
// other pear instances aren't coordinated, so with a server that rotates
// refresh tokens, a race between two instances can disconnect the member.
func (v *tokenVault) refresh(ctx context.Context, g grant, cfg *oauthConfig) (*storedToken, error) {
	// The refresh is shared by every caller waiting on it, and once the
	// authorization server has rotated the refresh token the new one must be
	// stored, so it isn't cut short when the caller that started it goes away.
	// refreshTimeout bounds it instead.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), refreshTimeout)
	defer cancel()
	shared, err, _ := v.refreshes.Do(g.key(), func() (any, error) {
		// Read the tokens again, rather than using the caller's copy: another
		// caller may have refreshed them since, rotating the refresh token the
		// caller read.
		current, err := v.get(ctx, g)
		if err != nil {
			return nil, err
		}
		if !current.needsRefresh() {
			return current, nil
		}
		if current.RefreshToken == "" {
			return nil, fmt.Errorf("%w: access token expired", ErrNotConnected)
		}
		token, err := cfg.refresh(ctx, v.client, current.RefreshToken)
		if errors.Is(err, errInvalidGrant) {
			slog.InfoContext(ctx, "mcp: refresh token rejected; disconnecting",
				"org", g.org.String(), "server", string(g.server), "did", g.did.String())
			if err := v.delete(ctx, g); err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("%w: sign-in expired", ErrNotConnected)
		} else if err != nil {
			return nil, err
		}
		if err := v.put(ctx, g, token); err != nil {
			return nil, err
		}
		return newStoredToken(g.generation, token), nil
	})
	if err != nil {
		return nil, err
	}
	// Every caller sharing the refresh gets its tokens, which only
	// singleflight's untyped result carries.
	refreshed, _ := shared.(*storedToken)
	return refreshed, nil
}
