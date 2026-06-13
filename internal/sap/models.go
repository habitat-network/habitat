package sap

import (
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	crawlStateRunning  = "running"
	crawlStateComplete = "complete"
	crawlStateErrored  = "errored"
)

type managedOrg struct {
	DID  syntax.DID `gorm:"column:did;primaryKey"`
	Host string

	// Pending auth
	State        *string `gorm:"index"`
	CodeVerifier *string

	// Completed auth
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time

	ErrorMsg        string
	CrawlState      *string
	SubscribeCursor string
	CrawlCursor     string
}

type repoState string

const (
	RepoStatePending   repoState = "pending"
	RepoStateResyncing repoState = "resyncing"
	RepoStateActive    repoState = "active"
	RepoStateDesynced  repoState = "desynced"
	RepoStateError     repoState = "error"
)

type managedRepo struct {
	Space habitat_syntax.SpaceURI `gorm:"primaryKey"`
	DID   syntax.DID              `gorm:"column:did;primaryKey"`
	Rev   syntax.TID
	State repoState `gorm:"index:idx_repos_state_retry"`

	ErrorMsg   string
	RetryCount int   `gorm:"not null;default:0"`
	RetryAfter int64 `gorm:"not null;default:0;index:idx_repos_state_retry"`
}

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
