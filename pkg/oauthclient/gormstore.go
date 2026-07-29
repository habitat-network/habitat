package oauthclient

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
	DID       string `gorm:"column:did;primaryKey;type:text"`
	SessionID string `gorm:"column:session_id;primaryKey;type:text"`
	Data      []byte `gorm:"column:data;type:blob"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (sessionRow) TableName() string { return "client_sessions" }

// authRequestRow stores oauth.AuthRequestData keyed by state.
type authRequestRow struct {
	State     string `gorm:"column:state;primaryKey;type:text"`
	Data      []byte `gorm:"column:data;type:blob"`
	CreatedAt time.Time
}

func (authRequestRow) TableName() string { return "client_auth_requests" }

type gormStore struct {
	db                  *gorm.DB
	singleSessionPerDID bool
}

var _ oauth.ClientAuthStore = (*gormStore)(nil)

// Option configures a gormStore created by NewGormStore.
type Option func(*gormStore)

// WithSingleSessionPerDID configures the store to keep at most one session
// per account DID: SaveSession replaces any existing session for the DID
// (even if it was saved under a different session ID), and GetSession /
// DeleteSession ignore their sessionID argument, operating on whichever
// session is currently stored for the DID.
//
// By default (this option omitted) the store keeps one row per (DID, session
// ID) pair, allowing multiple concurrent sessions per account.
func WithSingleSessionPerDID() Option {
	return func(s *gormStore) {
		s.singleSessionPerDID = true
	}
}

// NewGormStore creates a ClientAuthStore backed by GORM.
func NewGormStore(db *gorm.DB, opts ...Option) (oauth.ClientAuthStore, error) {
	if err := db.AutoMigrate(&sessionRow{}, &authRequestRow{}); err != nil {
		return nil, fmt.Errorf("migrate gormstore: %w", err)
	}
	s := &gormStore{db: db}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// GetSession implements oauth.ClientAuthStore.
func (s *gormStore) GetSession(
	ctx context.Context,
	did syntax.DID,
	sessionID string,
) (*oauth.ClientSessionData, error) {
	q := s.db.WithContext(ctx).Where("did = ?", did.String())
	if !s.singleSessionPerDID {
		q = q.Where("session_id = ?", sessionID)
	}
	var row sessionRow
	if err := q.First(&row).Error; err != nil {
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
	row := sessionRow{DID: key.DID, SessionID: key.SessionID, Data: data}
	if !s.singleSessionPerDID {
		return s.db.WithContext(ctx).Save(&row).Error
	}
	// A new session may be saved under a different session ID than any
	// existing row for this DID (indigo assigns the session ID per auth
	// flow), so replace by DID rather than upserting by primary key.
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("did = ?", key.DID).Delete(&sessionRow{}).Error; err != nil {
			return err
		}
		return tx.Create(&row).Error
	})
}

// DeleteSession implements oauth.ClientAuthStore.
func (s *gormStore) DeleteSession(ctx context.Context, did syntax.DID, sessionID string) error {
	q := s.db.WithContext(ctx).Where("did = ?", did.String())
	if !s.singleSessionPerDID {
		q = q.Where("session_id = ?", sessionID)
	}
	return q.Delete(&sessionRow{}).Error
}

// GetAuthRequestInfo implements oauth.ClientAuthStore.
func (s *gormStore) GetAuthRequestInfo(
	ctx context.Context,
	state string,
) (*oauth.AuthRequestData, error) {
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
