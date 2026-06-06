package login

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/encrypt"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
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
	did syntax.DID,
	loginId string,
) (string, []byte, error) {
	if loginId == "" {
		return "", nil, fmt.Errorf("no google email configured for org %s", did)
	}

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
		oauth2.SetAuthURLParam("login_hint", loginId),
		oauth2.SetAuthURLParam("prompt", "select_account"),
	)

	return authURL, stateBytes, nil
}

func (p *googleProvider) Exchange(
	ctx context.Context,
	_ syntax.DID,
	loginId string,
	code string,
	_ string,
	stateBytes []byte,
) error {
	var s googleProviderState
	if err := json.Unmarshal(stateBytes, &s); err != nil {
		return fmt.Errorf("unmarshal google state: %w", err)
	}

	token, err := p.oauthCfg.Exchange(ctx, code, oauth2.VerifierOption(s.Verifier))
	if err != nil {
		return fmt.Errorf("google token exchange: %w", err)
	}

	idToken, ok := token.Extra("id_token").(string)
	if !ok || idToken == "" {
		return fmt.Errorf("no id_token in google token response")
	}

	email, err := verifyGoogleIDToken(idToken, p.oauthCfg.ClientID)
	if err != nil {
		return fmt.Errorf("verify google id token: %w", err)
	}

	if err := p.upsertCredentials(ctx, loginId, &Credentials{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
		IDToken:      idToken,
		Email:        email,
	}); err != nil {
		return fmt.Errorf("store google credentials: %w", err)
	}

	return nil
}

type googleCredentialsModel struct {
	Email        string `gorm:"primarykey"`
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

type googleIDTokenClaims struct {
	Iss           string `json:"iss"`
	Aud           string `json:"aud"`
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	Iat           int64  `json:"iat"`
	Exp           int64  `json:"exp"`
}

func verifyGoogleIDToken(idToken, clientID string) (string, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid id token: expected 3 segments, got %d", len(parts))
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode id token payload: %w", err)
	}

	var claims googleIDTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("parse id token claims: %w", err)
	}

	if claims.Iss != "https://accounts.google.com" && claims.Iss != "accounts.google.com" {
		return "", fmt.Errorf("unexpected id token issuer: %s", claims.Iss)
	}
	if claims.Aud != clientID {
		return "", fmt.Errorf(
			"id token audience mismatch: got %s, expected %s",
			claims.Aud,
			clientID,
		)
	}
	if claims.Exp > 0 && time.Now().Unix() > claims.Exp {
		return "", fmt.Errorf("id token expired")
	}
	if !claims.EmailVerified {
		return "", fmt.Errorf("google email not verified")
	}
	if claims.Email == "" {
		return "", fmt.Errorf("no email in google id token")
	}

	return claims.Email, nil
}
