package login

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"

	"github.com/habitat-network/habitat/internal/keyval"
	"github.com/habitat-network/habitat/internal/utils"
)

type Credentials struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
	IDToken      string
	Email        string
}

type googleProvider struct {
	oauthCfg *oauth2.Config
	creds    keyval.EncryptedStore
	kv       keyval.Store
}

// loginStateTTL bounds how long a provider's flow state stays redeemable. It
// matches the OAuth session cookie's MaxAge (oauthserver), so a stored entry
// never outlives the cookie that references it.
const loginStateTTL = 10 * time.Minute

type googleProviderState struct {
	Verifier string `json:"verifier"`
	State    string `json:"state"`
}

func NewGoogleProvider(
	clientID, clientSecret, redirectURL string,
	creds keyval.EncryptedStore,
	kv keyval.Store,
) (Provider, error) {
	return &googleProvider{
		oauthCfg: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
				TokenURL: "https://oauth2.googleapis.com/token",
			},
		},
		creds: creds,
		kv:    kv,
	}, nil
}

func (p *googleProvider) Authorize(
	ctx context.Context,
	loginHint string,
) (string, string, string, error) {
	verifier := oauth2.GenerateVerifier()
	state := make([]byte, 16)
	if _, err := rand.Read(state); err != nil {
		return "", "", "", fmt.Errorf("generate state: %w", err)
	}
	stateStr := hex.EncodeToString(state)
	key := utils.RandomNonce(32)
	if err := p.kv.Put(
		ctx, key, googleProviderState{Verifier: verifier, State: stateStr}, loginStateTTL,
	); err != nil {
		return "", "", "", fmt.Errorf("store google state: %w", err)
	}
	authURL := p.oauthCfg.AuthCodeURL(
		stateStr,
		oauth2.AccessTypeOffline,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("login_hint", loginHint),
		oauth2.SetAuthURLParam("prompt", "select_account"),
	)

	return authURL, stateStr, key, nil
}

func (p *googleProvider) Exchange(
	ctx context.Context,
	query url.Values,
	providerStateKey string,
) (loginID string, profile Profile, err error) {
	code := query.Get("code")
	var s googleProviderState
	// Consume, not Get: the state is single-use, so replaying the callback fails.
	if err := p.kv.Consume(ctx, providerStateKey, &s); err != nil {
		return "", Profile{}, fmt.Errorf("google state: %w", err)
	}
	if s.State != query.Get("state") {
		return "", Profile{}, fmt.Errorf("google state mismatch")
	}
	token, err := p.oauthCfg.Exchange(ctx, code, oauth2.VerifierOption(s.Verifier))
	if err != nil {
		return "", Profile{}, fmt.Errorf("google token exchange: %w", err)
	}

	idToken, ok := token.Extra("id_token").(string)
	if !ok || idToken == "" {
		return "", Profile{}, fmt.Errorf("no id_token in google token response")
	}

	claims, err := verifyGoogleIDToken(idToken, p.oauthCfg.ClientID)
	if err != nil {
		return "", Profile{}, fmt.Errorf("verify google id token: %w", err)
	}

	if err := p.upsertCredentials(ctx, claims.Email, &Credentials{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
		IDToken:      idToken,
		Email:        claims.Email,
	}); err != nil {
		return "", Profile{}, fmt.Errorf("store google credentials: %w", err)
	}

	return claims.Email, Profile{Name: claims.Name, Picture: claims.Picture}, nil
}

func (p *googleProvider) upsertCredentials(
	ctx context.Context,
	email string,
	creds *Credentials,
) error {
	if err := p.creds.PutEncrypted(ctx, email, creds, keyval.NoExpiry); err != nil {
		return fmt.Errorf("save google credentials: %w", err)
	}
	return nil
}

func (p *googleProvider) GetCredentials(
	ctx context.Context,
	email string,
) (*Credentials, error) {
	var creds Credentials
	if err := p.creds.GetEncrypted(ctx, email, &creds); err != nil {
		return nil, fmt.Errorf("google credentials not found: %w", err)
	}
	return &creds, nil
}

// googleIDTokenLeeway is the clock-skew allowance applied to iat/exp
// comparisons against wall-clock now.
const googleIDTokenLeeway = 60 * time.Second

type googleIDTokenClaims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	jwt.RegisteredClaims
}

// verifyGoogleIDToken decodes idToken's claims and checks them: audience,
// expiration/issued-at (via jwt/v5's claim validator), issuer, and that the
// email is present and verified. It does not verify the token's signature:
// idToken comes straight from Google's token endpoint in the server-to-server
// code exchange above (oauthCfg.Exchange, authenticated with our client
// secret), not from a redirect or POST an untrusted party could tamper with.
func verifyGoogleIDToken(idToken, clientID string) (googleIDTokenClaims, error) {
	var claims googleIDTokenClaims
	if _, _, err := jwt.NewParser().ParseUnverified(idToken, &claims); err != nil {
		return googleIDTokenClaims{}, fmt.Errorf("parse id token: %w", err)
	}
	validator := jwt.NewValidator(
		jwt.WithAudience(clientID),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(googleIDTokenLeeway),
	)
	if err := validator.Validate(claims); err != nil {
		return googleIDTokenClaims{}, fmt.Errorf("validate id token claims: %w", err)
	}
	// Google issues ID tokens under two issuer spellings, so this can't use
	// jwt.WithIssuer, which accepts only one.
	if claims.Issuer != "https://accounts.google.com" && claims.Issuer != "accounts.google.com" {
		return googleIDTokenClaims{}, fmt.Errorf("unexpected id token issuer: %s", claims.Issuer)
	}
	if !claims.EmailVerified {
		return googleIDTokenClaims{}, fmt.Errorf("google email not verified")
	}
	if claims.Email == "" {
		return googleIDTokenClaims{}, fmt.Errorf("no email in google id token")
	}
	return claims, nil
}
