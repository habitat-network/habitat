package googlecred

import (
	"context"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/encrypt"
	"gorm.io/gorm"
)

type GoogleCredentialStore interface {
	UpsertCredentials(ctx context.Context, did syntax.DID, credentials *Credentials) error
	GetCredentials(ctx context.Context, did syntax.DID) (*Credentials, error)
}

type Credentials struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
	IDToken      string
	Email        string
}

func NewGoogleCredentialStore(db *gorm.DB, encryptionKey []byte) (GoogleCredentialStore, error) {
	if encryptionKey == nil {
		return nil, fmt.Errorf("encryption key is required")
	}
	if err := db.AutoMigrate(&googleCredentialsModel{}); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}
	return &googleCredentialStore{db: db, encryptionKey: encryptionKey}, nil
}

type googleCredentialsModel struct {
	DID          string `gorm:"column:did;primarykey"`
	AccessToken  string // encrypted
	RefreshToken string // encrypted
	Expiry       time.Time
	IDToken      string // encrypted
	Email        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type googleCredentialStore struct {
	db            *gorm.DB
	encryptionKey []byte
}

func (s *googleCredentialStore) UpsertCredentials(
	ctx context.Context,
	did syntax.DID,
	creds *Credentials,
) error {
	m := &googleCredentialsModel{DID: did.String()}
	var err error
	if m.AccessToken, err = encrypt.EncryptCBOR(creds.AccessToken, s.encryptionKey); err != nil {
		return fmt.Errorf("encrypt access token: %w", err)
	}
	if m.RefreshToken, err = encrypt.EncryptCBOR(creds.RefreshToken, s.encryptionKey); err != nil {
		return fmt.Errorf("encrypt refresh token: %w", err)
	}
	if m.IDToken, err = encrypt.EncryptCBOR(creds.IDToken, s.encryptionKey); err != nil {
		return fmt.Errorf("encrypt id token: %w", err)
	}
	m.Expiry = creds.Expiry
	m.Email = creds.Email
	if err := s.db.WithContext(ctx).Save(m).Error; err != nil {
		return fmt.Errorf("save google credentials: %w", err)
	}
	return nil
}

func (s *googleCredentialStore) GetCredentials(
	ctx context.Context,
	did syntax.DID,
) (*Credentials, error) {
	var m googleCredentialsModel
	if err := s.db.WithContext(ctx).Where("did = ?", did).First(&m).Error; err != nil {
		return nil, fmt.Errorf("google credentials not found: %w", err)
	}
	var accessToken, refreshToken, idToken string
	if err := encrypt.DecryptCBOR(m.AccessToken, s.encryptionKey, &accessToken); err != nil {
		return nil, fmt.Errorf("decrypt access token: %w", err)
	}
	if err := encrypt.DecryptCBOR(m.RefreshToken, s.encryptionKey, &refreshToken); err != nil {
		return nil, fmt.Errorf("decrypt refresh token: %w", err)
	}
	if err := encrypt.DecryptCBOR(m.IDToken, s.encryptionKey, &idToken); err != nil {
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
