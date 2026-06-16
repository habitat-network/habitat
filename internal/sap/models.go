package sap

import (
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// managedOrg is the GORM model for organization auth and crawl state
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

	ErrorMsg string

	Cursor string
}

type repoState string

const (
	RepoStatePending  repoState = "pending"
	RepoStateActive   repoState = "active"
	RepoStateDesynced repoState = "desynced"
)

// managedRepo is the GORM model for repository sync state
type managedRepo struct {
	Space habitat_syntax.SpaceURI `gorm:"primaryKey"`
	DID   syntax.DID              `gorm:"column:did;primaryKey"`
	Rev   syntax.TID
	State repoState `gorm:"index"`
}

// outbox is the GORM model for outbox events to be sent to the client
type outbox struct {
	ID        string `gorm:"primaryKey;autoIncrement"`
	URI       habitat_syntax.SpaceRecordURI
	Value     []byte
	CreatedAt time.Time
	AckedAt   *time.Time
}

func autoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&managedOrg{}, &managedRepo{}, &outbox{})
}
