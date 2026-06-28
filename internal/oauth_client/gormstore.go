package oauth_client

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
)

// sessionRow stores oauth.ClientSessionData keyed by (did, session_id).
type sessionRow struct {
	DID       string    `gorm:"column:did;primaryKey;type:text"`
	SessionID string    `gorm:"column:session_id;primaryKey;type:text"`
	Data      []byte    `gorm:"column:data;type:blob"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (sessionRow) TableName() string { return "client_sessions" }

// authRequestRow stores oauth.AuthRequestData keyed by state.
type authRequestRow struct {
	State     string    `gorm:"column:state;primaryKey;type:text"`
	Data      []byte    `gorm:"column:data;type:blob"`
	CreatedAt time.Time
}

func (authRequestRow) TableName() string { return "client_auth_requests" }

type gormStore struct {
	db *gorm.DB
}

var _ oauth.ClientAuthStore = (*gormStore)(nil)

// NewGormStore creates a ClientAuthStore backed by GORM.
func NewGormStore(db *gorm.DB) (oauth.ClientAuthStore, error) {
	if err := db.AutoMigrate(&sessionRow{}, &authRequestRow{}); err != nil {
		return nil, fmt.Errorf("migrate gormstore: %w", err)
	}
	return &gormStore{db: db}, nil
}

// GetSession implements oauth.ClientAuthStore.
func (s *gormStore) GetSession(ctx context.Context, did syntax.DID, sessionID string) (*oauth.ClientSessionData, error) {
	var row sessionRow
	if err := s.db.WithContext(ctx).
		Where("did = ? AND session_id = ?", did.String(), sessionID).
		First(&row).Error; err != nil {
		return nil, err
	}
	var data oauth.ClientSessionData
	if err := json.Unmarshal(row.Data, &data); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}
	return &data, nil
}

// SaveSession implements oauth.ClientAuthStore.
func (s *gormStore) SaveSession(ctx context.Context, sess oauth.ClientSessionData) error {
	data, err := json.Marshal(sess)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	key, err := extractSessionKey(data)
	if err != nil {
		return fmt.Errorf("extract session key: %w", err)
	}
	return s.db.WithContext(ctx).Save(&sessionRow{
		DID:       key.DID,
		SessionID: key.SessionID,
		Data:      data,
	}).Error
}

// DeleteSession implements oauth.ClientAuthStore.
func (s *gormStore) DeleteSession(ctx context.Context, did syntax.DID, sessionID string) error {
	return s.db.WithContext(ctx).
		Where("did = ? AND session_id = ?", did.String(), sessionID).
		Delete(&sessionRow{}).Error
}

// GetAuthRequestInfo implements oauth.ClientAuthStore.
func (s *gormStore) GetAuthRequestInfo(ctx context.Context, state string) (*oauth.AuthRequestData, error) {
	var row authRequestRow
	if err := s.db.WithContext(ctx).
		Where("state = ?", state).
		First(&row).Error; err != nil {
		return nil, err
	}
	var data oauth.AuthRequestData
	if err := json.Unmarshal(row.Data, &data); err != nil {
		return nil, fmt.Errorf("unmarshal auth request: %w", err)
	}
	return &data, nil
}

// SaveAuthRequestInfo implements oauth.ClientAuthStore.
func (s *gormStore) SaveAuthRequestInfo(ctx context.Context, info oauth.AuthRequestData) error {
	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("marshal auth request: %w", err)
	}
	return s.db.WithContext(ctx).Create(&authRequestRow{
		State: info.State,
		Data:  data,
	}).Error
}

// DeleteAuthRequestInfo implements oauth.ClientAuthStore.
func (s *gormStore) DeleteAuthRequestInfo(ctx context.Context, state string) error {
	return s.db.WithContext(ctx).
		Where("state = ?", state).
		Delete(&authRequestRow{}).Error
}

// sessionKey holds the primary-key fields extracted from ClientSessionData JSON.
type sessionKey struct {
	DID       string `json:"account_did"`
	SessionID string `json:"session_id"`
}

func extractSessionKey(data []byte) (*sessionKey, error) {
	var key sessionKey
	if err := json.Unmarshal(data, &key); err != nil {
		return nil, err
	}
	if key.DID == "" || key.SessionID == "" {
		return nil, fmt.Errorf("session data missing did or session_id")
	}
	return &key, nil
}
