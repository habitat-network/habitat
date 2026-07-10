package sap

import (
	"context"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
)

// loginManager persists the in-progress user login flows. The resulting session
// is stored as an ordinary managed DID (orgManager) — sap does not track users
// separately — so this only records the redirect and the authenticated DID that
// let the docs server complete its server-session login.
type loginManager struct {
	db *gorm.DB
}

func newLoginManager(db *gorm.DB) *loginManager {
	return &loginManager{db: db}
}

// SaveLoginFlow records a pending user login keyed by its OAuth state, so the
// callback can recognise it as a user (redirect-back) login and knows where to
// send the browser afterwards.
func (m *loginManager) SaveLoginFlow(ctx context.Context, state, redirectURL string) error {
	return m.db.WithContext(ctx).
		Save(&loginFlow{State: state, RedirectURL: redirectURL}).
		Error
}

// GetLoginFlow returns the pending login for a state, or false if none exists
// (i.e. the callback belongs to an org-admin bootstrap, which has no redirect).
func (m *loginManager) GetLoginFlow(ctx context.Context, state string) (*loginFlow, bool, error) {
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
func (m *loginManager) CompleteLoginFlow(ctx context.Context, state string, did syntax.DID) error {
	return m.db.WithContext(ctx).
		Model(&loginFlow{}).
		Where("state = ?", state).
		Update("did", did).
		Error
}

// GetCompletedLogin returns the DID that authenticated for a completed login
// flow, identified by the login token (the OAuth state) handed to the redirect
// target.
func (m *loginManager) GetCompletedLogin(ctx context.Context, state string) (syntax.DID, error) {
	var flow loginFlow
	if err := m.db.WithContext(ctx).Where("state = ?", state).First(&flow).Error; err != nil {
		return "", err
	}
	return flow.DID, nil
}
