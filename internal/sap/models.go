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

// userSession tracks the OAuth session sap holds for an individual user (as
// opposed to a managed org). Unlike managedOrg it triggers no crawling or
// firehose subscription; it exists solely so services (e.g. the docs server)
// can proxy pear calls authenticated as that user. Keyed by DID so the latest
// login wins.
type userSession struct {
	DID       syntax.DID `gorm:"column:did;primaryKey"`
	SessionID string     // OAuth session ID, keys the oauth_client session store
	CreatedAt time.Time
	UpdatedAt time.Time
}

// loginFlow records an in-progress user OAuth flow started via StartUserLogin,
// keyed by the OAuth state (which is also the resulting session ID). Its
// presence at callback time is what distinguishes a user login from an org
// login, and it carries the URL to redirect the browser back to once the flow
// completes. DID is filled in after the callback so the redirect target can
// resolve which user authenticated.
type loginFlow struct {
	State       string `gorm:"column:state;primaryKey"`
	RedirectURL string
	DID         syntax.DID `gorm:"column:did"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
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
		&userSession{},
		&loginFlow{},
	)
}
