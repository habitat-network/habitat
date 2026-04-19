package main

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Session struct {
	ID                 string `gorm:"primaryKey"`
	DID                string `gorm:"column:did;uniqueIndex;not null"`
	GoogleAccessToken  string
	GoogleRefreshToken string
	TokenExpiry        int64
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type Store struct {
	db *gorm.DB
}

func NewStore(dbPath string) (*Store, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	if err := db.AutoMigrate(&Session{}); err != nil {
		return nil, err
	}

	return &Store{db: db}, nil
}

func (s *Store) CreateSession(did string) (*Session, error) {
	sessionID, err := generateSessionID()
	if err != nil {
		return nil, err
	}

	session := &Session{
		ID:        sessionID,
		DID:       did,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := s.db.Create(session).Error; err != nil {
		return nil, err
	}

	return session, nil
}

func (s *Store) GetSession(sessionID string) (*Session, error) {
	var session Session
	err := s.db.Where("id = ?", sessionID).First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *Store) GetSessionByDID(did string) (*Session, error) {
	var session Session
	err := s.db.Where("did = ?", did).First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *Store) UpdateTokens(sessionID, accessToken, refreshToken string, expiry int64) error {
	return s.db.Model(&Session{}).
		Where("id = ?", sessionID).
		Updates(map[string]interface{}{
			"google_access_token":  accessToken,
			"google_refresh_token": refreshToken,
			"token_expiry":         expiry,
			"updated_at":           time.Now(),
		}).Error
}

func (s *Store) DeleteSession(sessionID string) error {
	return s.db.Delete(&Session{}, "id = ?", sessionID).Error
}

func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func generateSessionID() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
