package pdscred

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"errors"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/eagraf/habitat-new/internal/encrypt"
	"github.com/eagraf/habitat-new/internal/oauthclient"
	"gorm.io/gorm"
)

type PDSCredentialStore interface {
	UpsertCredentials(did syntax.DID, dpopKey []byte, tokenInfo *oauthclient.TokenResponse) error
	GetDpopClient(did syntax.DID) (*oauthclient.DpopHttpClient, error)
}

func NewPDSCredentialStore(db *gorm.DB, encryptionKey []byte) (PDSCredentialStore, error) {
	if encryptionKey == nil {
		return nil, fmt.Errorf("encryption key is required")
	}

	// Run migrations
	if err := db.AutoMigrate(&pdsCredentials{}); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return &pdsCredentialStore{
		db:            db,
		encryptionKey: encryptionKey,
	}, nil
}

type pdsCredentialStore struct {
	db            *gorm.DB
	encryptionKey []byte
}

// pdsCredentials stores PDS credentials for a user (DID).
// These credentials are reused across multiple OAuth sessions.
// Updated on every sign-in.
type pdsCredentials struct {
	DID string `gorm:"column:did;primarykey"` // DID of the user (primary key)

	// Tokens received from the AT Protocol PDS (via OAuth client)
	AccessToken  string // Access token from PDS
	RefreshToken string // Refresh token from PDS
	TokenType    string
	Scope        string // Space-separated scopes from PDS

	// DPoP key for proof-of-possession
	DpopKey []byte // Serialized ECDSA private key

	CreatedAt time.Time
	UpdatedAt time.Time
}

// GetDpopClient implements [PDSCredentialStore].
func (p *pdsCredentialStore) GetDpopClient(did syntax.DID) (*oauthclient.DpopHttpClient, error) {
	var creds pdsCredentials
	err := p.db.Where("did = ?", did).First(&creds).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("user credentials not found for DID %s", did)
		}
		return nil, fmt.Errorf("failed to get user credentials: %w", err)
	}

	// Decrypt the access token
	var decryptedAccessToken string
	if err := encrypt.DecryptCBOR(creds.AccessToken, p.encryptionKey, &decryptedAccessToken); err != nil {
		return nil, fmt.Errorf("failed to decrypt access token: %w", err)
	}

	dpopKey, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), creds.DpopKey)
	if err != nil {
		return nil, fmt.Errorf("failed to parse dpop key: %w", err)
	}
	return oauthclient.NewDpopHttpClient(
		dpopKey,
		&oauthclient.MemoryNonceProvider{},
		oauthclient.WithAccessToken(decryptedAccessToken),
	), nil
}

// UpsertCredentials implements [PDSCredentialStore].
func (p *pdsCredentialStore) UpsertCredentials(
	did syntax.DID,
	dpopKey []byte,
	tokenInfo *oauthclient.TokenResponse,
) error {
	// Find or create user credentials
	var userCreds pdsCredentials
	err := p.db.Where("did = ?", did).First(&userCreds).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Create new user credentials
			userCreds = pdsCredentials{
				DID: did.String(),
			}
		} else {
			return fmt.Errorf("failed to look up user credentials: %w", err)
		}
	}

	// Update the PDS credentials (on every sign-in)
	// Encrypt tokens before storing
	encryptedAccessToken, err := encrypt.EncryptCBOR(tokenInfo.AccessToken, p.encryptionKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt access token: %w", err)
	}
	encryptedRefreshToken, err := encrypt.EncryptCBOR(tokenInfo.RefreshToken, p.encryptionKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt refresh token: %w", err)
	}

	userCreds.AccessToken = encryptedAccessToken
	userCreds.RefreshToken = encryptedRefreshToken
	userCreds.TokenType = tokenInfo.TokenType
	userCreds.Scope = tokenInfo.Scope
	userCreds.DpopKey = dpopKey

	// Save the user credentials (upsert)
	if err := p.db.Save(&userCreds).Error; err != nil {
		return fmt.Errorf("failed to save user credentials: %w", err)
	}

	return nil
}
