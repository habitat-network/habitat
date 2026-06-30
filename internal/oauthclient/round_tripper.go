package oauthclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
	"golang.org/x/sync/singleflight"
)

type roundTripper struct {
	sess        *oauth.ClientSessionData
	tokenExpiry time.Time
	store       oauth.ClientAuthStore
	config      *oauth.ClientConfig
	did         syntax.DID
	sessionID   string
	refreshG    *singleflight.Group
}

var _ http.RoundTripper = (*roundTripper)(nil)

func NewClient(
	ctx context.Context,
	store oauth.ClientAuthStore,
	config *oauth.ClientConfig,
	did syntax.DID,
	sessionID string,
	refreshG *singleflight.Group,
) (*http.Client, error) {
	sess, err := store.GetSession(ctx, did, sessionID)
	if err != nil {
		return nil, err
	}
	expiry, err := getExpiry(sess.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("parse token expiry: %w", err)
	}
	return &http.Client{
		Transport: &roundTripper{
			sess:        sess,
			tokenExpiry: expiry,
			store:       store,
			config:      config,
			did:         did,
			sessionID:   sessionID,
			refreshG:    refreshG,
		},
	}, nil
}

// RoundTrip implements [http.RoundTripper].
func (r *roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Habitat-Auth-Method", "oauth")
	hostURL, err := url.Parse(r.sess.HostURL)
	if err != nil {
		return nil, err
	}
	req.URL = hostURL.ResolveReference(req.URL)
	return new(oauth2.Transport{
		Source: oauth2.ReuseTokenSource(r.getToken(), &tokenSource{
			rt:  r,
			ctx: req.Context(),
		}),
	}).RoundTrip(req)
}

func (r *roundTripper) getToken() *oauth2.Token {
	return &oauth2.Token{
		AccessToken:  r.sess.AccessToken,
		RefreshToken: r.sess.RefreshToken,
		Expiry:       r.tokenExpiry,
	}
}

type tokenSource struct {
	ctx context.Context
	rt  *roundTripper
}

var _ oauth2.TokenSource = (*tokenSource)(nil)

// Token implements [oauth2.TokenSource].
func (t *tokenSource) Token() (*oauth2.Token, error) {
	token, err, _ := t.rt.refreshG.Do(t.rt.sessionID, func() (any, error) {
		refreshedToken, err := internalConfig(
			t.rt.config,
			t.rt.sess.HostURL,
		).TokenSource(t.ctx, t.rt.getToken()).Token()
		if err != nil {
			return "", err
		}

		if refreshedToken.AccessToken != t.rt.sess.AccessToken {
			expiry, err := getExpiry(refreshedToken.AccessToken)
			if err != nil {
				return "", fmt.Errorf("parse token expiry: %w", err)
			}
			t.rt.tokenExpiry = expiry
			t.rt.sess.AccessToken = refreshedToken.AccessToken
			t.rt.sess.RefreshToken = refreshedToken.RefreshToken
			if err := t.rt.store.SaveSession(t.ctx, *t.rt.sess); err != nil {
				return "", err
			}
		}
		return refreshedToken, nil
	})
	if err != nil {
		return nil, err
	}
	return token.(*oauth2.Token), nil
}

// getExpiry parses the expiry from a JWT access token.
func getExpiry(accessToken string) (time.Time, error) {
	var claims jwt.MapClaims
	if _, _, err := jwt.NewParser().ParseUnverified(accessToken, &claims); err != nil {
		return time.Time{}, err
	}
	exp, err := claims.GetExpirationTime()
	if err != nil {
		return time.Time{}, err
	}
	return exp.Time, nil
}
