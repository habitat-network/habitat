package sap

import (
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
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

	ErrorMsg string

	Cursor string
}
