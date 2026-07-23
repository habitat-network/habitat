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

// repoState tracks where a managed repository is in its sync lifecycle.
type repoState string

// Repository sync states. These represent the lifecycle of a managed repo
// from initial discovery through active sync and error recovery.
const (
	// RepoStatePending indicates a repo has been discovered but not yet
	// synced.
	RepoStatePending repoState = "pending"
	// RepoStateResyncing indicates a repo is currently being backfilled.
	RepoStateResyncing repoState = "resyncing"
	// RepoStateActive indicates a repo is fully synced and receiving live
	// events.
	RepoStateActive repoState = "active"
	// RepoStateDesynced indicates a repo fell behind its live events and
	// needs a full resync.
	RepoStateDesynced repoState = "desynced"
	// RepoStateError indicates the last sync attempt failed and the repo
	// will be retried after a backoff period.
	RepoStateError repoState = "error"
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
	)
}
