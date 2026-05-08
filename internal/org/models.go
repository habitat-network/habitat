package org

import "time"

type ID string

// Keep track of members in the org
type member struct {
	ID           ID `gorm:"primaryKey"`
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
