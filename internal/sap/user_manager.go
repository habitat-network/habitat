package sap

import (
	"context"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
)

// userManager persists the OAuth sessions sap holds for individual users and
// the in-progress login flows that produce them. It is deliberately separate
// from orgManager: user sessions carry no crawl or subscription state, they
// only let a trusted service proxy pear calls as the user.
type userManager struct {
	db *gorm.DB
}

func newUserManager(db *gorm.DB) *userManager {
	return &userManager{db: db}
}

// SaveLoginFlow records a pending user login keyed by its OAuth state, so the
// callback can recognise it as a user (not org) login and knows where to send
// the browser afterwards.
func (m *userManager) SaveLoginFlow(ctx context.Context, state, redirectURL string) error {
	return m.db.WithContext(ctx).
		Save(&loginFlow{State: state, RedirectURL: redirectURL}).
		Error
}

// GetLoginFlow returns the pending login for a state, or false if none exists
// (i.e. the callback belongs to an org login).
func (m *userManager) GetLoginFlow(ctx context.Context, state string) (*loginFlow, bool, error) {
	var flow loginFlow
	err := m.db.WithContext(ctx).Where("state = ?", state).First(&flow).Error
	if err == gorm.ErrRecordNotFound {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &flow, true, nil
}

// CompleteLoginFlow records which user authenticated for a flow, so the docs
// server can resolve the login token it receives on the redirect back to a DID.
func (m *userManager) CompleteLoginFlow(ctx context.Context, state string, did syntax.DID) error {
	return m.db.WithContext(ctx).
		Model(&loginFlow{}).
		Where("state = ?", state).
		Update("did", did).
		Error
}

// GetCompletedLogin returns the DID that authenticated for a completed login
// flow, identified by the login token (the OAuth state) handed to the redirect
// target.
func (m *userManager) GetCompletedLogin(ctx context.Context, state string) (syntax.DID, error) {
	var flow loginFlow
	if err := m.db.WithContext(ctx).Where("state = ?", state).First(&flow).Error; err != nil {
		return "", err
	}
	return flow.DID, nil
}

// AddUserSession upserts the OAuth session sap tracks for a user DID, so the
// latest login is the one proxied requests authenticate with.
func (m *userManager) AddUserSession(ctx context.Context, did syntax.DID, sessionID string) error {
	return m.db.WithContext(ctx).
		Save(&userSession{DID: did, SessionID: sessionID}).
		Error
}

// GetUserSession returns the tracked session for a user DID.
func (m *userManager) GetUserSession(ctx context.Context, did syntax.DID) (*userSession, error) {
	var sess userSession
	if err := m.db.WithContext(ctx).Where("did = ?", did).First(&sess).Error; err != nil {
		return nil, err
	}
	return &sess, nil
}
