package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
)

type orgCredential struct {
	Org  syntax.DID `gorm:"primaryKey"`
	Host string

	// Pending auth
	State        *string `gorm:"index"`
	CodeVerifier *string

	// Completed auth
	AccessToken  string `gorm:"type:text"`
	RefreshToken string `gorm:"type:text"`
	ExpiresAt    time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type orgManager struct {
	db     *gorm.DB
	domain string
	secret atcrypto.PrivateKey
}

func newOrgManager(db *gorm.DB, domain string, secret atcrypto.PrivateKey) (*orgManager, error) {
	err := db.AutoMigrate(&orgCredential{})
	if err != nil {
		return nil, err
	}
	return &orgManager{db: db, domain: domain, secret: secret}, nil
}

func (o *orgManager) InitiateAuth(
	ctx context.Context,
	orgHandle string,
) (redirectURL string, err error) {
	verifier := oauth2.GenerateVerifier()
	b := make([]byte, 16)
	rand.Read(b)
	state := base64.URLEncoding.EncodeToString(b)
	atid, err := syntax.ParseAtIdentifier(orgHandle)
	if err != nil {
		return "", fmt.Errorf("parse handle %q: %w", orgHandle, err)
	}

	id, err := identity.DefaultDirectory().Lookup(ctx, atid)
	if err != nil {
		return "", fmt.Errorf("lookup %q: %w", orgHandle, err)
	}

	host := id.GetServiceEndpoint("habitat")
	if host == "" {
		return "", fmt.Errorf("no habitat service endpoint for %q", orgHandle)
	}
	if err := o.db.WithContext(ctx).Save(&orgCredential{
		Org:          id.DID,
		Host:         host,
		State:        &state,
		CodeVerifier: &verifier,
	}).Error; err != nil {
		return "", fmt.Errorf("save pending state: %w", err)
	}

	config := o.oauthConfig(host)
	authorizeURL := config.AuthCodeURL(state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("handle", orgHandle),
	)

	return authorizeURL, nil
}

func (o *orgManager) CompleteAuth(ctx context.Context, code, state string) (syntax.DID, error) {
	var pending orgCredential
	if err := o.db.WithContext(ctx).Where("state = ?", state).First(&pending).Error; err != nil {
		return "", fmt.Errorf("pending state not found: %w", err)
	}

	config := o.oauthConfig(pending.Host)
	token, err := config.Exchange(
		ctx,
		code,
		oauth2.SetAuthURLParam("code_verifier", *pending.CodeVerifier),
	)
	if err != nil {
		return "", fmt.Errorf("exchange code: %w", err)
	}

	if err := o.db.Save(&orgCredential{
		Org:          pending.Org,
		Host:         pending.Host,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.Expiry,
	}).Error; err != nil {
		return "", fmt.Errorf("save refreshed token: %w", err)
	}

	return pending.Org, nil
}

var ErrOrgNotFound = fmt.Errorf("no org")

func (o *orgManager) GetOrgs(ctx context.Context) ([]syntax.DID, error) {
	var creds []orgCredential
	err := o.db.WithContext(ctx).
		Where("access_token != ''").
		Where("expires_at > ?", time.Now()).
		Find(&creds).
		Error
	if err != nil {
		return nil, err
	}
	var orgs []syntax.DID
	for _, cred := range creds {
		orgs = append(orgs, cred.Org)
	}
	return orgs, nil
}

func (o *orgManager) ClientMetadata() (*oauth.ClientMetadata, error) {
	config := oauth.NewPublicConfig("https://"+o.domain+"/client-metadata.json",
		"https://"+o.domain+"/oauth-callback", []string{})
	err := config.SetClientSecret(o.secret, "sap")
	if err != nil {
		return nil, err
	}
	cm := config.ClientMetadata()
	return &cm, nil
}

func (o *orgManager) oauthConfig(hostURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:    "https://" + o.domain + "/client-metadata.json",
		RedirectURL: "https://" + o.domain + "/oauth-callback",
		Endpoint: oauth2.Endpoint{
			TokenURL: hostURL + "/oauth/token",
			AuthURL:  hostURL + "/oauth/authorize",
		},
	}
}

type tokenSource struct {
	did syntax.DID
	a   *orgManager
}

func (o *orgManager) GetTokenSource(orgID syntax.DID) oauth2.TokenSource {
	return &tokenSource{a: o, did: orgID}
}

// Token implements [oauth2.TokenSource].
func (t *tokenSource) Token() (*oauth2.Token, error) {
	var cred orgCredential
	if err := t.a.db.Where("org = ?", t.did).First(&cred).Error; err != nil {
		return nil, err
	}
	refreshedToken, err := t.a.oauthConfig(cred.Host).
		TokenSource(context.Background(), &oauth2.Token{
			AccessToken:  cred.AccessToken,
			RefreshToken: cred.RefreshToken,
			Expiry:       cred.ExpiresAt,
		}).
		Token() // checks expiry and refreshes token
	if err != nil {
		return nil, err
	}
	if refreshedToken.AccessToken != cred.AccessToken {
		if err := t.a.db.Save(&orgCredential{
			Org:          cred.Org,
			Host:         cred.Host,
			AccessToken:  refreshedToken.AccessToken,
			RefreshToken: refreshedToken.RefreshToken,
			ExpiresAt:    refreshedToken.Expiry,
		}).Error; err != nil {
			return nil, err
		}
	}

	return refreshedToken, nil
}
