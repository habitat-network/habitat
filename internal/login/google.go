package login

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/encrypt"
)

type Credentials struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
	IDToken      string
	Email        string
}

type googleProvider struct {
	oauthCfg      *oauth2.Config
	db            *gorm.DB
	encryptionKey []byte
}

type googleProviderState struct {
	Verifier string `json:"verifier"`
	State    string `json:"state"`
}

func NewGoogleProvider(
	clientID, clientSecret, redirectURL string,
	db *gorm.DB,
	encryptionKey []byte,
) (Provider, error) {
	if encryptionKey == nil {
		return nil, fmt.Errorf("encryption key is required")
	}
	if err := db.AutoMigrate(&googleCredentialsModel{}); err != nil {
		return nil, fmt.Errorf("migrate google credentials table: %w", err)
	}
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
		db:            db,
		encryptionKey: encryptionKey,
	}, nil
}

func (p *googleProvider) Authorize(
	ctx context.Context,
	loginHint string,
) (string, []byte, error) {
	verifier := oauth2.GenerateVerifier()
	state := make([]byte, 16)
	if _, err := rand.Read(state); err != nil {
		return "", nil, fmt.Errorf("generate state: %w", err)
	}
	stateStr := hex.EncodeToString(state)
	stateBytes, err := json.Marshal(googleProviderState{Verifier: verifier, State: stateStr})
	if err != nil {
		return "", nil, fmt.Errorf("marshal google state: %w", err)
	}
	authURL := p.oauthCfg.AuthCodeURL(
		stateStr,
		oauth2.AccessTypeOffline,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("login_hint", loginHint),
		oauth2.SetAuthURLParam("prompt", "select_account"),
	)

	return authURL, stateBytes, nil
}

func (p *googleProvider) Exchange(
	ctx context.Context,
	query url.Values,
	stateBytes []byte,
) (loginID string, err error) {
	code := query.Get("code")
	var s googleProviderState
	if err := json.Unmarshal(stateBytes, &s); err != nil {
		return "", fmt.Errorf("unmarshal google state: %w", err)
	}
	if s.State != query.Get("state") {
		return "", fmt.Errorf("google state mismatch")
	}
	token, err := p.oauthCfg.Exchange(ctx, code, oauth2.VerifierOption(s.Verifier))
	if err != nil {
		return "", fmt.Errorf("google token exchange: %w", err)
	}

	idToken, ok := token.Extra("id_token").(string)
	if !ok || idToken == "" {
		return "", fmt.Errorf("no id_token in google token response")
	}

	claims, err := verifyGoogleIDToken(idToken, p.oauthCfg.ClientID)
	if err != nil {
		return "", fmt.Errorf("verify google id token: %w", err)
	}

	if err := p.upsertCredentials(ctx, claims.Email, &Credentials{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
		IDToken:      idToken,
		Email:        claims.Email,
	}); err != nil {
		return "", fmt.Errorf("store google credentials: %w", err)
	}

	return claims.Email, nil
}

type googleCredentialsModel struct {
	Email        string `gorm:"primaryKey"`
	AccessToken  string // encrypted
	RefreshToken string // encrypted
	Expiry       time.Time
	IDToken      string // encrypted
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (p *googleProvider) upsertCredentials(
	ctx context.Context,
	email string,
	creds *Credentials,
) error {
	m := &googleCredentialsModel{Email: email}
	var err error
	if m.AccessToken, err = encrypt.EncryptCBOR(creds.AccessToken, p.encryptionKey); err != nil {
		return fmt.Errorf("encrypt access token: %w", err)
	}
	if m.RefreshToken, err = encrypt.EncryptCBOR(creds.RefreshToken, p.encryptionKey); err != nil {
		return fmt.Errorf("encrypt refresh token: %w", err)
	}
	if m.IDToken, err = encrypt.EncryptCBOR(creds.IDToken, p.encryptionKey); err != nil {
		return fmt.Errorf("encrypt id token: %w", err)
	}
	m.Expiry = creds.Expiry
	m.Email = creds.Email
	if err := p.db.WithContext(ctx).Save(m).Error; err != nil {
		return fmt.Errorf("save google credentials: %w", err)
	}
	return nil
}

func (p *googleProvider) GetCredentials(
	ctx context.Context,
	email string,
) (*Credentials, error) {
	var m googleCredentialsModel
	if err := p.db.WithContext(ctx).Where("email = ?", email).First(&m).Error; err != nil {
		return nil, fmt.Errorf("google credentials not found: %w", err)
	}
	var accessToken, refreshToken, idToken string
	if err := encrypt.DecryptCBOR(m.AccessToken, p.encryptionKey, &accessToken); err != nil {
		return nil, fmt.Errorf("decrypt access token: %w", err)
	}
	if err := encrypt.DecryptCBOR(m.RefreshToken, p.encryptionKey, &refreshToken); err != nil {
		return nil, fmt.Errorf("decrypt refresh token: %w", err)
	}
	if err := encrypt.DecryptCBOR(m.IDToken, p.encryptionKey, &idToken); err != nil {
		return nil, fmt.Errorf("decrypt id token: %w", err)
	}
	return &Credentials{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		Expiry:       m.Expiry,
		IDToken:      idToken,
		Email:        m.Email,
	}, nil
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
