package pdsclient

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/encrypt"
	"gorm.io/gorm"
)

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
}

func (oauthAuthRequestModel) TableName() string { return "oauth_auth_requests" }

type OAuthStore struct {
	db            *gorm.DB
	encryptionKey []byte
}

var _ oauth.ClientAuthStore = (*OAuthStore)(nil)

func NewOAuthStore(db *gorm.DB, encryptionKey []byte) (*OAuthStore, error) {
	if err := db.AutoMigrate(&oauthSessionModel{}, &oauthAuthRequestModel{}); err != nil {
		return nil, fmt.Errorf("migrate oauth store: %w", err)
	}
	return &OAuthStore{db: db, encryptionKey: encryptionKey}, nil
}

func (s *OAuthStore) SaveSession(ctx context.Context, sess oauth.ClientSessionData) error {
	data, err := json.Marshal(sess)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	encrypted, err := encrypt.EncryptCBOR(string(data), s.encryptionKey)
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
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}
	var decrypted string
	if err := encrypt.DecryptCBOR(m.Data, s.encryptionKey, &decrypted); err != nil {
		return nil, fmt.Errorf("decrypt session: %w", err)
	}
	var sess oauth.ClientSessionData
	if err := json.Unmarshal([]byte(decrypted), &sess); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
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
	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("marshal auth request: %w", err)
	}
	encrypted, err := encrypt.EncryptCBOR(string(data), s.encryptionKey)
	if err != nil {
		return fmt.Errorf("encrypt auth request: %w", err)
	}
	m := oauthAuthRequestModel{
		State:     info.State,
		Data:      encrypted,
		ExpiresAt: time.Now().Add(10 * time.Minute),
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
	var decrypted string
	if err := encrypt.DecryptCBOR(m.Data, s.encryptionKey, &decrypted); err != nil {
		return nil, fmt.Errorf("decrypt auth request: %w", err)
	}
	var info oauth.AuthRequestData
	if err := json.Unmarshal([]byte(decrypted), &info); err != nil {
		return nil, fmt.Errorf("unmarshal auth request: %w", err)
	}
	return &info, nil
}

func (s *OAuthStore) DeleteAuthRequestInfo(ctx context.Context, state string) error {
	return s.db.WithContext(ctx).Where("state = ?", state).Delete(&oauthAuthRequestModel{}).Error
}
