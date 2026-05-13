package org

import "time"

// Organization represents a managed org on a pear instance.
type Organization struct {
	ID            string `gorm:"primaryKey"`
	Name          string // optional display name
	Subdomain     string // subdomain hosted under
	SigningSecret string // base64-encoded HMAC-SHA256 key for invite tokens
	CreatedAt     time.Time
}

// Keep track of members in the org.
// Each member belongs to exactly one org.
type member struct {
	OrgID        string `gorm:"primaryKey"`
	Member       string `gorm:"primaryKey"`
	Role         string
	PasswordHash string `gorm:"not null"`

	// Automatically populated by gorm
	CreatedAt time.Time
}

// spentToken tracks consumed single-use invite tokens by their JWT ID, scoped per org.
type spentToken struct {
	OrgID      string    `gorm:"primaryKey"`
	JTI        string    `gorm:"primaryKey"`
	ConsumedAt time.Time `gorm:"not null"`
}
