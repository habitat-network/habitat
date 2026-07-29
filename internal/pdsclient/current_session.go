package pdsclient

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
)

// currentSessionModel tracks which OAuth session ID is the currently active
// one for a given account DID. Habitat keeps a single active PDS OAuth
// session per account for login purposes; the actual session data (tokens,
// DPoP key) lives in the shared oauthclient store, keyed by (DID, session
// ID). This tiny pointer table lets Do resolve the right session ID for a
// DID without the caller needing to track it explicitly.
type currentSessionModel struct {
	DID       string `gorm:"column:did;primaryKey"`
	SessionID string `gorm:"column:session_id"`
}

func (currentSessionModel) TableName() string { return "pds_current_sessions" }

type currentSessionStore struct {
	db *gorm.DB
}

func newCurrentSessionStore(db *gorm.DB) (*currentSessionStore, error) {
	if err := db.AutoMigrate(&currentSessionModel{}); err != nil {
		return nil, fmt.Errorf("migrate current session store: %w", err)
	}
	return &currentSessionStore{db: db}, nil
}

// Set records sessionID as the current session for did.
func (s *currentSessionStore) Set(ctx context.Context, did syntax.DID, sessionID string) error {
	return s.db.WithContext(ctx).Save(&currentSessionModel{
		DID:       did.String(),
		SessionID: sessionID,
	}).Error
}

// Get returns the current session ID for did.
func (s *currentSessionStore) Get(ctx context.Context, did syntax.DID) (string, error) {
	var m currentSessionModel
	if err := s.db.WithContext(ctx).Where("did = ?", did.String()).First(&m).Error; err != nil {
		return "", fmt.Errorf("get current session for %s: %w", did, err)
	}
	return m.SessionID, nil
}
