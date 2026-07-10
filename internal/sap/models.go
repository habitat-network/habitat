package sap

import (
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

type crawlState string

const (
	crawlStateRunning  crawlState = "running"
	crawlStateComplete crawlState = "complete"
	crawlStateErrored  crawlState = "errored"
)

type managedOrg struct {
	DID       syntax.DID `gorm:"column:did;primaryKey"`
	SessionID string     // OAuth session ID, keys the oauth_client session store
	CreatedAt time.Time
	UpdatedAt time.Time

	ErrorMsg        string
	CrawlState      *crawlState
	SubscribeCursor string
	CrawlCursor     string
}

// loginFlow records an in-progress user OAuth flow started via StartUserLogin,
// keyed by the OAuth state (which is also the resulting session ID). sap does
// not distinguish orgs from users — both are managed DIDs with sessions that it
// crawls — but a user login differs from the org-admin bootstrap in that it
// redirects the browser back to the service that started it. loginFlow carries
// that redirect URL, and its DID (filled in after the callback) lets the
// redirect target resolve which user authenticated.
type loginFlow struct {
	State       string `gorm:"column:state;primaryKey"`
	RedirectURL string
	DID         syntax.DID `gorm:"column:did"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// crawledSpace records that a space's repos have been enumerated, so a space
// shared between managed DIDs (e.g. an org and one of its members) is only
// crawled once even though it shows up in both DIDs' listSpaces.
type crawledSpace struct {
	Space     habitat_syntax.SpaceURI `gorm:"primaryKey"`
	CreatedAt time.Time
}

type repoState string

const (
	RepoStatePending   repoState = "pending"
	RepoStateResyncing repoState = "resyncing"
	RepoStateActive    repoState = "active"
	RepoStateDesynced  repoState = "desynced"
	RepoStateError     repoState = "error"
)

// managedRepo is the GORM model for repository sync state
type managedRepo struct {
	Space habitat_syntax.SpaceURI `gorm:"primaryKey"`
	DID   syntax.DID              `gorm:"column:did;primaryKey"`
	Rev   syntax.TID
	State repoState `gorm:"index"`

	ErrorMsg   string
	RetryCount int   `gorm:"not null;default:0"`
	RetryAfter int64 `gorm:"not null;default:0;index"`
}

// outbox is the GORM model for outbox events to be sent to the client
type outbox struct {
	ID        uint `gorm:"primaryKey;autoIncrement"`
	URI       habitat_syntax.SpaceRecordURI
	Value     []byte
	CreatedAt time.Time
	AckedAt   *time.Time
}

type bufferedEvent struct {
	ID    uint                    `gorm:"primaryKey"`
	Space habitat_syntax.SpaceURI `gorm:"index:idx_resync_space_repo"`
	DID   syntax.DID              `gorm:"column:did;index:idx_resync_space_repo"`
	Seq   uint64
	Data  []byte
}

func autoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&managedOrg{},
		&managedRepo{},
		&outbox{},
		&bufferedEvent{},
		&loginFlow{},
		&crawledSpace{},
	)
}
