package main

import (
	"fmt"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type SpaceState struct {
	Org       string `gorm:"primaryKey"`
	Space     string `gorm:"primaryKey"`
	SpaceType string
	State     string // active, backfilling, resyncing, desynchronized, error
	SpaceRev  string
	MemberRev string
	LastRev   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type RepoState struct {
	Space string `gorm:"primaryKey"`
	Repo  string `gorm:"primaryKey"`
	Rev   string
	State string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type RecordState struct {
	Space      string `gorm:"primaryKey"`
	Repo       string `gorm:"primaryKey"`
	Collection string `gorm:"primaryKey"`
	Rkey       string `gorm:"primaryKey"`
	Rev        string
	Record     []byte
}

type MemberState struct {
	Space  string `gorm:"primaryKey"`
	DID    string `gorm:"primaryKey"`
	Access string
	Rev    string
}

type OutboxEvent struct {
	ID        uint   `gorm:"primaryKey;autoIncrement"`
	EventJSON string `gorm:"type:text"`
	Acked     bool   `gorm:"index"`
	Attempts  int
	CreatedAt time.Time
	UpdatedAt time.Time
}

func openDB(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.AutoMigrate(&SpaceState{}, &RepoState{}, &RecordState{}, &MemberState{}, &OutboxEvent{}); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}
