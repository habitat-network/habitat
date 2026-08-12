package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/habitat-network/habitat/pkg/oauthclient"
)

// oauthClient is the subset of *oauthclient.Client sessionResolver depends
// on: resuming a previously-established session, and minting one via the
// RFC 7523 JWT-bearer grant.
type oauthClient interface {
	ResumeSession(
		ctx context.Context,
		did syntax.DID,
		sessionID string,
	) (*oauth.ClientSession, error)
	SendJWTTokenRequest(ctx context.Context, identifier string) (*oauth.ClientSessionData, error)
}

// Auth methods a tracked session can use to authenticate to its host.
const (
	authOAuth     = "oauth"
	authJWTBearer = "jwt-bearer"
)

// trackedSession records which method sap should use to authenticate as a
// DID, and — once minted — the resulting OAuth session ID. This is the
// mapping session.Clients needs but pkg/sap doesn't own: sap only asks
// ClientForDID for a client, never how a session came to exist.
type trackedSession struct {
	DID       string `gorm:"column:did;primaryKey"`
	SessionID string // resumable once set; empty for jwt-bearer until minted
	Method    string // authOAuth or authJWTBearer
}

func (trackedSession) TableName() string { return "sap_tracked_sessions" }

// sessionResolver implements session.Clients: it satisfies sap's "give me a
// client for this DID" requests using whichever auth method /session/add or
// /session/jwt recorded for that DID.
type sessionResolver struct {
	db     *gorm.DB
	client oauthClient
}

func newSessionResolver(db *gorm.DB, client oauthClient) (*sessionResolver, error) {
	if err := db.AutoMigrate(&trackedSession{}); err != nil {
		return nil, fmt.Errorf("migrate tracked sessions: %w", err)
	}
	return &sessionResolver{db: db, client: client}, nil
}

// recordOAuthSession records that did authenticates via a completed browser
// OAuth flow, resumable under sessionID.
func (r *sessionResolver) recordOAuthSession(
	ctx context.Context,
	did syntax.DID,
	sessionID string,
) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "did"}},
		DoUpdates: clause.AssignmentColumns([]string{"session_id", "method"}),
	}).Create(&trackedSession{DID: did.String(), SessionID: sessionID, Method: authOAuth}).Error
}

// recordJWTBearerSession records that did authenticates via the JWT-bearer
// grant. No session exists yet — ClientForDID mints one lazily on first use.
func (r *sessionResolver) recordJWTBearerSession(ctx context.Context, did syntax.DID) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "did"}},
		DoUpdates: clause.AssignmentColumns([]string{"method"}),
	}).Create(&trackedSession{DID: did.String(), Method: authJWTBearer}).Error
}

// ClientForDID implements session.Clients.
func (r *sessionResolver) ClientForDID(ctx context.Context, did syntax.DID) (*http.Client, error) {
	var tracked trackedSession
	if err := r.db.WithContext(ctx).First(&tracked, "did = ?", did.String()).Error; err != nil {
		return nil, fmt.Errorf("load tracked session for %s: %w", did, err)
	}
	if tracked.Method == authJWTBearer {
		return r.jwtBearerClient(ctx, did, tracked.SessionID)
	}
	sess, err := r.client.ResumeSession(ctx, did, tracked.SessionID)
	if err != nil {
		return nil, err
	}
	return oauthclient.SessionClient(sess), nil
}

// jwtBearerClient resumes did's previously-minted jwt-bearer session, or
// mints one and persists its session ID for reuse if none exists yet (or the
// one on record no longer resumes, e.g. it expired).
func (r *sessionResolver) jwtBearerClient(
	ctx context.Context,
	did syntax.DID,
	sessionID string,
) (*http.Client, error) {
	if sessionID != "" {
		if sess, err := r.client.ResumeSession(ctx, did, sessionID); err == nil {
			return oauthclient.SessionClient(sess), nil
		}
	}
	sessData, err := r.client.SendJWTTokenRequest(ctx, did.String())
	if err != nil {
		return nil, fmt.Errorf("mint jwt-bearer session for %s: %w", did, err)
	}
	if err := r.db.WithContext(ctx).Model(&trackedSession{}).
		Where("did = ?", did.String()).
		Update("session_id", sessData.SessionID).Error; err != nil {
		return nil, fmt.Errorf("save jwt-bearer session id: %w", err)
	}
	sess, err := r.client.ResumeSession(ctx, did, sessData.SessionID)
	if err != nil {
		return nil, err
	}
	return oauthclient.SessionClient(sess), nil
}
