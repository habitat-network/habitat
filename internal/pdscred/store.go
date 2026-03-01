package pdscred

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/encrypt"
	"gorm.io/gorm"
)

type PDSCredentialStore interface {
	UpsertCredentials(did syntax.DID, credentials *Credentials) error
	GetCredentials(did syntax.DID) (*Credentials, error)
}

func NewPDSCredentialStore(
	db *gorm.DB,
	encryptionKey []byte,
) (PDSCredentialStore, error) {
	if encryptionKey == nil {
		return nil, fmt.Errorf("encryption key is required")
	}
	// Run migrations
	if err := db.AutoMigrate(&pdsCredentialsModel{}); err != nil {
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

// pdsCredentialsModel stores PDS credentials for a user (DID).
// These credentials are reused across multiple OAuth sessions.
// Updated on every sign-in.
type pdsCredentialsModel struct {
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

type Credentials struct {
	AccessToken  string
	RefreshToken string
	DpopKey      *ecdsa.PrivateKey
}

func (p *pdsCredentialStore) GetCredentials(did syntax.DID) (*Credentials, error) {
	var creds pdsCredentialsModel
	err := p.db.Where("did = ?", did).First(&creds).Error
	if err != nil {
		return nil, fmt.Errorf("user credentials not found: %w", err)
	}
	// Decrypt the access token
	var decryptedAccessToken string
	if err := encrypt.DecryptCBOR(creds.AccessToken, p.encryptionKey, &decryptedAccessToken); err != nil {
		return nil, fmt.Errorf("failed to decrypt access token: %w", err)
	}

	// Decrypt the refresh token
	var decryptedRefreshToken string
	if err := encrypt.DecryptCBOR(creds.RefreshToken, p.encryptionKey, &decryptedRefreshToken); err != nil {
		return nil, fmt.Errorf("failed to decrypt refresh token: %w", err)
	}

	dpopKey, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), creds.DpopKey)
	if err != nil {
		return nil, fmt.Errorf("failed to parse dpop key: %w", err)
	}
	return &Credentials{
		AccessToken:  decryptedAccessToken,
		RefreshToken: decryptedRefreshToken,
		DpopKey:      dpopKey,
	}, nil
}

// UpsertCredentials implements [PDSCredentialStore].
func (p *pdsCredentialStore) UpsertCredentials(
	did syntax.DID,
	tokenInfo *Credentials,
) error {
	// Find or create user credentials
	userCreds := &pdsCredentialsModel{
		DID: did.String(),
	}
	// Encrypt tokens before storing
	var err error
	if userCreds.AccessToken, err = encrypt.EncryptCBOR(tokenInfo.AccessToken, p.encryptionKey); err != nil {
		return fmt.Errorf("failed to encrypt access token: %w", err)
	}
	if userCreds.RefreshToken, err = encrypt.EncryptCBOR(tokenInfo.RefreshToken, p.encryptionKey); err != nil {
		return fmt.Errorf("failed to encrypt refresh token: %w", err)
	}
	if userCreds.DpopKey, err = tokenInfo.DpopKey.Bytes(); err != nil {
		return fmt.Errorf("failed to get dpop key bytes: %w", err)
	}
	// Save the user credentials (upsert)
	if err := p.db.Save(&userCreds).Error; err != nil {
		return fmt.Errorf("failed to save user credentials: %w", err)
	}
	return nil
}
