package pdsclient

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/encrypt"
	"gorm.io/gorm"
)

const defaultAuthRequestTTL = 10 * time.Minute

type oauthSessionModel struct {
	DID       string `gorm:"column:did;primaryKey"`
	SessionID string `gorm:"column:session_id;primaryKey"`
	Data      string `gorm:"column:data;type:text"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (oauthSessionModel) TableName() string { return "oauth_sessions" }

type oauthAuthRequestModel struct {
	State     string    `gorm:"column:state;primaryKey"`
	Data      string    `gorm:"column:data;type:text"`
	ExpiresAt time.Time `gorm:"column:expires_at;index"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (oauthAuthRequestModel) TableName() string { return "oauth_auth_requests" }

type OAuthStore struct {
	db            *gorm.DB
	encryptionKey []byte
}

var _ oauth.ClientAuthStore = (*OAuthStore)(nil)

func NewOAuthStore(db *gorm.DB, encryptionKey []byte) (*OAuthStore, error) {
	if encryptionKey == nil {
		return nil, fmt.Errorf("encryption key is required")
	}
	if err := db.AutoMigrate(&oauthSessionModel{}, &oauthAuthRequestModel{}); err != nil {
		return nil, fmt.Errorf("migrate oauth store: %w", err)
	}
	return &OAuthStore{db: db, encryptionKey: encryptionKey}, nil
}

func (s *OAuthStore) SaveSession(ctx context.Context, sess oauth.ClientSessionData) error {
	encrypted, err := encrypt.EncryptCBOR(sess, s.encryptionKey)
	if err != nil {
		return fmt.Errorf("encrypt session: %w", err)
	}
	m := oauthSessionModel{
		DID:       sess.AccountDID.String(),
		SessionID: sess.SessionID,
		Data:      encrypted,
	}
	return s.db.WithContext(ctx).Save(&m).Error
}

func (s *OAuthStore) GetSession(
	ctx context.Context,
	did syntax.DID,
	sessionID string,
) (*oauth.ClientSessionData, error) {
	var m oauthSessionModel
	err := s.db.WithContext(ctx).
		Where("did = ? AND session_id = ?", did.String(), sessionID).
		First(&m).
		Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("session not found: %w", err)
	} else if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	var sess oauth.ClientSessionData
	if err := encrypt.DecryptCBOR(m.Data, s.encryptionKey, &sess); err != nil {
		return nil, fmt.Errorf("decrypt session: %w", err)
	}
	return &sess, nil
}

func (s *OAuthStore) DeleteSession(ctx context.Context, did syntax.DID, sessionID string) error {
	return s.db.WithContext(ctx).
		Where("did = ? AND session_id = ?", did.String(), sessionID).
		Delete(&oauthSessionModel{}).
		Error
}

func (s *OAuthStore) SaveAuthRequestInfo(ctx context.Context, info oauth.AuthRequestData) error {
	encrypted, err := encrypt.EncryptCBOR(info, s.encryptionKey)
	if err != nil {
		return fmt.Errorf("encrypt auth request: %w", err)
	}
	m := oauthAuthRequestModel{
		State:     info.State,
		Data:      encrypted,
		ExpiresAt: time.Now().Add(defaultAuthRequestTTL),
	}
	return s.db.WithContext(ctx).Create(&m).Error
}

func (s *OAuthStore) GetAuthRequestInfo(
	ctx context.Context,
	state string,
) (*oauth.AuthRequestData, error) {
	var m oauthAuthRequestModel
	err := s.db.WithContext(ctx).Where("state = ?", state).First(&m).Error
	if err != nil {
		return nil, fmt.Errorf("auth request not found: %w", err)
	}
	var info oauth.AuthRequestData
	if err := encrypt.DecryptCBOR(m.Data, s.encryptionKey, &info); err != nil {
		return nil, fmt.Errorf("decrypt auth request: %w", err)
	}
	if m.ExpiresAt.Before(time.Now()) {
		_ = s.db.WithContext(ctx).Delete(&m).Error // best-effort cleanup
		return nil, fmt.Errorf("auth request not found: %w", gorm.ErrRecordNotFound)
	}
	return &info, nil
}

func (s *OAuthStore) DeleteAuthRequestInfo(ctx context.Context, state string) error {
	return s.db.WithContext(ctx).Where("state = ?", state).Delete(&oauthAuthRequestModel{}).Error
}

func (s *OAuthStore) DeleteExpiredAuthRequests(ctx context.Context) error {
	return s.db.WithContext(ctx).
		Where("expires_at < ?", time.Now()).
		Delete(&oauthAuthRequestModel{}).
		Error
}
