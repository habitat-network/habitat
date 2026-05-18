package org

import "time"

type LoginMethod string

const (
	LoginMethodAtproto  LoginMethod = "atproto"
	LoginMethodGoogle   LoginMethod = "google"
	LoginMethodPassword LoginMethod = "password"
)

// organization represents a managed org on a pear instance.
type organization struct {
	ID            string      `gorm:"primaryKey"`
	Name          string      // optional display name
	LoginMethod   LoginMethod // "atproto", "google", "password"
	SigningSecret string      // base64-encoded HMAC-SHA256 key for invite tokens
	CreatedAt     time.Time
}

// Keep track of members in the org.
// Each Member belongs to exactly one org.
type Member struct {
	OrgID   string `gorm:"primaryKey"`
	Member  string `gorm:"primaryKey"`
	Role    string `gorm:"not null"`
	LoginID string `gorm:"not null"` // provider-specific identifier (password hash, public ATProto DID, google email, etc.)

	// Automatically populated by gorm
	CreatedAt time.Time
}

// spentToken tracks consumed single-use invite tokens by their JWT ID, scoped per org.
type spentToken struct {
	OrgID      string    `gorm:"primaryKey"`
	JTI        string    `gorm:"primaryKey"`
	ConsumedAt time.Time `gorm:"not null"`
}
