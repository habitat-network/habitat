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

// managedOrg is a managed account sap holds credentials for and syncs on behalf
// of. Despite the name it is any user/account (not only orgs): sap authenticates
// to a host as this account via OAuth. Multiple managed accounts may be able to
// see the same space; which repos they surface is deduped by managedRepo.
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

// managedSpace associates a space with a managed account that can access it
// (discovered when that account's listSpaces returned the space). A space may
// have several such accounts; the resyncer uses any one of them's OAuth
// credentials to pull the space's repos.
type managedSpace struct {
	Space habitat_syntax.SpaceURI `gorm:"primaryKey"`
	DID   syntax.DID              `gorm:"column:did;primaryKey"` // managed account DID
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
		&managedSpace{},
		&managedRepo{},
		&outbox{},
		&bufferedEvent{},
	)
}
