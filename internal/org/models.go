package org

import (
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type loginMethod string

const (
	LoginMethodAtproto  loginMethod = "atproto"
	LoginMethodGoogle   loginMethod = "google"
	LoginMethodPassword loginMethod = "password"
)

// organization represents a managed org on a pear instance.
type organization struct {
	ID              syntax.DID  `gorm:"primaryKey"`
	Name            string      // optional display name
	LoginMethod     loginMethod // "atproto", "google", "password"
	SigningSecret   string      // base64-encoded HMAC-SHA256 key for invite tokens
	CreatedAt       time.Time
	HandleSubdomain string `gorm:"unique"`
}

// Keep track of members in the org.
// Each member belongs to exactly one org.
type member struct {
	OrgID        syntax.DID   `gorm:"primaryKey"`
	Organization organization `gorm:"foreignKey:OrgID"`
	Did          syntax.DID   `gorm:"primaryKey"`
	Role         Role         `gorm:"not null"`
	LoginID      string       `gorm:"not null"` // provider-specific identifier (password hash, public ATProto DID, google email, etc.)

	// Automatically populated by gorm
	CreatedAt time.Time
}

// spentToken tracks consumed single-use invite tokens by their JWT ID, scoped per org.
type spentToken struct {
	OrgID      syntax.DID `gorm:"primaryKey"`
	JTI        string     `gorm:"primaryKey"`
	ConsumedAt time.Time  `gorm:"not null"`
}
