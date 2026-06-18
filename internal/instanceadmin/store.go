package instanceadmin

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/alexedwards/argon2id"
	"gorm.io/gorm"
)

const sessionDuration = 24 * time.Hour

// Store manages the single instance admin account and its sessions.
type Store interface {
	// Bootstrap ensures the instance admin account exists. If it does not exist yet,
	// it is created using presetPassword if non-empty, otherwise a randomly generated
	// password. The plaintext password is returned only when created is true and
	// presetPassword was empty (i.e. it was generated, not operator-supplied).
	Bootstrap(ctx context.Context, presetPassword string) (password string, created bool, err error)

	// Authenticate checks the given password against the stored admin account.
	Authenticate(ctx context.Context, password string) (bool, error)

	// CreateSession creates a new session for the instance admin and returns its token
	// and expiry.
	CreateSession(ctx context.Context) (token string, expiresAt time.Time, err error)

	// ValidateSession reports whether the given session token is valid and unexpired.
	ValidateSession(ctx context.Context, token string) (bool, error)

	// DeleteSession removes the given session, if present.
	DeleteSession(ctx context.Context, token string) error
}

type storeImpl struct {
	db *gorm.DB
}

var _ Store = (*storeImpl)(nil)

func NewStore(db *gorm.DB) (Store, error) {
	if err := db.AutoMigrate(&instanceAdminAccount{}, &instanceAdminSession{}); err != nil {
		return nil, err
	}
	return &storeImpl{db: db}, nil
}

func generatePassword() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *storeImpl) Bootstrap(
	ctx context.Context,
	presetPassword string,
) (string, bool, error) {
	var existing instanceAdminAccount
	err := s.db.WithContext(ctx).First(&existing, instanceAdminAccountID).Error
	if err == nil {
		return "", false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, err
	}

	generated := presetPassword == ""
	password := presetPassword
	if generated {
		password, err = generatePassword()
		if err != nil {
			return "", false, err
		}
	}

	hash, err := argon2id.CreateHash(password, argon2id.DefaultParams)
	if err != nil {
		return "", false, err
	}

	account := instanceAdminAccount{
		ID:           instanceAdminAccountID,
		PasswordHash: hash,
		CreatedAt:    time.Now(),
	}
	if err := s.db.WithContext(ctx).Create(&account).Error; err != nil {
		return "", false, err
	}

	if !generated {
		return "", true, nil
	}
	return password, true, nil
}

func (s *storeImpl) Authenticate(ctx context.Context, password string) (bool, error) {
	var account instanceAdminAccount
	if err := s.db.WithContext(ctx).First(&account, instanceAdminAccountID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	ok, err := argon2id.ComparePasswordAndHash(password, account.PasswordHash)
	if errors.Is(err, argon2id.ErrInvalidHash) {
		return false, nil
	}
	return ok, err
}

func (s *storeImpl) CreateSession(ctx context.Context) (string, time.Time, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", time.Time{}, err
	}
	token := hex.EncodeToString(b)
	expiresAt := time.Now().Add(sessionDuration)

	session := instanceAdminSession{
		Token:     token,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
	}
	if err := s.db.WithContext(ctx).Create(&session).Error; err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

func (s *storeImpl) ValidateSession(ctx context.Context, token string) (bool, error) {
	var session instanceAdminSession
	err := s.db.WithContext(ctx).Where("token = ?", token).First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if session.ExpiresAt.Before(time.Now()) {
		if err := s.DeleteSession(ctx, token); err != nil {
			return false, err
		}
		return false, nil
	}
	return true, nil
}

func (s *storeImpl) DeleteSession(ctx context.Context, token string) error {
	return s.db.WithContext(ctx).Where("token = ?", token).Delete(&instanceAdminSession{}).Error
}
