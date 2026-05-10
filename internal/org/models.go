package org

import "time"

// Keep track of members in the org
type member struct {
	Member       string `gorm:"primaryKey"`
	Role         string
	PasswordHash string `gorm:"not null"`

	// Automatically populated by gorm
	CreatedAt time.Time
}

// spentToken tracks consumed single-use invite tokens by their JWT ID.
type spentToken struct {
	JTI        string    `gorm:"primaryKey"`
	ConsumedAt time.Time `gorm:"not null"`
}
