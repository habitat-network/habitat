package sap

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
)

type orgManager struct {
	db     *gorm.DB
	domain string
	secret atcrypto.PrivateKey
	dir    identity.Directory
}

func newOrgManager(
	db *gorm.DB,
	domain string,
	secret atcrypto.PrivateKey,
	dir identity.Directory,
) *orgManager {
	return &orgManager{db: db, domain: domain, secret: secret, dir: dir}
}

func (o *orgManager) AddOrg(
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

	id, err := o.dir.Lookup(ctx, atid)
	if err != nil {
		return "", fmt.Errorf("lookup %q: %w", orgHandle, err)
	}

	host := id.GetServiceEndpoint("habitat")
	if host == "" {
		return "", fmt.Errorf("no habitat service endpoint for %q", orgHandle)
	}
	if err := o.db.WithContext(ctx).Save(&managedOrg{
		DID:          id.DID,
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

func (o *orgManager) completeAuth(ctx context.Context, code, state string) (*managedOrg, error) {
	var pending managedOrg
	if err := o.db.WithContext(ctx).Where("state = ?", state).First(&pending).Error; err != nil {
		return nil, fmt.Errorf("pending state not found: %w", err)
	}

	config := o.oauthConfig(pending.Host)
	token, err := config.Exchange(
		ctx,
		code,
		oauth2.SetAuthURLParam("code_verifier", *pending.CodeVerifier),
	)
	if err != nil {
		return nil, fmt.Errorf("exchange code: %w", err)
	}

	addedOrg := &managedOrg{
		DID:          pending.DID,
		Host:         pending.Host,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.Expiry,
	}
	if err := o.db.Save(addedOrg).Error; err != nil {
		return nil, fmt.Errorf("save refreshed token: %w", err)
	}
	return addedOrg, nil
}

var ErrOrgNotFound = fmt.Errorf("no org")

func (o *orgManager) ListOrgs(ctx context.Context) ([]syntax.DID, error) {
	var creds []managedOrg
	err := o.db.WithContext(ctx).
		Where("access_token != ''").
		Find(&creds).
		Error
	if err != nil {
		return nil, err
	}
	var orgs []syntax.DID
	for _, cred := range creds {
		orgs = append(orgs, cred.DID)
	}
	return orgs, nil
}

func (o *orgManager) clientMetadata() (*oauth.ClientMetadata, error) {
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
			TokenURL:  hostURL + "/oauth/token",
			AuthURL:   hostURL + "/oauth/authorize",
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}
}

type tokenSource struct {
	did syntax.DID
	o   *orgManager
}

func (o *orgManager) GetClient(ctx context.Context, orgDID syntax.DID) *http.Client {
	return oauth2.NewClient(ctx, &tokenSource{o: o, did: orgDID})
}

// Token implements [oauth2.TokenSource].
func (t *tokenSource) Token() (*oauth2.Token, error) {
	var cred managedOrg
	if err := t.o.db.Where("did = ?", t.did).First(&cred).Error; err != nil {
		return nil, err
	}
	refreshedToken, err := t.o.oauthConfig(cred.Host).
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
		if err := t.o.db.Save(&managedOrg{
			DID:          cred.DID,
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
