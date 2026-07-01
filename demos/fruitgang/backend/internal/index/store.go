package index

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Store struct {
	db *gorm.DB
}

var ErrNoDefaultSpace = fmt.Errorf("no default space configured")

func NewStore(db *gorm.DB) (*Store, error) {
	if err := db.AutoMigrate(&Member{}, &Chat{}, &ChatReply{}, &Log{}, &DefaultSpace{}); err != nil {
		return nil, fmt.Errorf("migrate index tables: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) UpsertMember(m Member) error {
	m.IndexedAt = time.Now()
	return s.db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&m).Error
}

func (s *Store) UpsertChat(c Chat) error {
	c.IndexedAt = time.Now()
	return s.db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&c).Error
}

func (s *Store) UpsertChatReply(r ChatReply) error {
	r.IndexedAt = time.Now()
	return s.db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&r).Error
}

func (s *Store) UpsertLog(l Log) error {
	l.IndexedAt = time.Now()
	return s.db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&l).Error
}

func (s *Store) GetMembers() ([]Member, error) {
	var out []Member
	return out, s.db.Order("created_at ASC").Find(&out).Error
}

func (s *Store) GetChats() ([]Chat, error) {
	var out []Chat
	return out, s.db.Order("created_at DESC").Find(&out).Error
}

func (s *Store) GetReplies(chatURI string) ([]ChatReply, error) {
	var out []ChatReply
	return out, s.db.Where("reply_to = ?", chatURI).Order("created_at ASC").Find(&out).Error
}

func (s *Store) GetLogs() ([]Log, error) {
	var out []Log
	return out, s.db.Order("created_at DESC").Find(&out).Error
}

func (s *Store) SetDefaultSpace(orgDID, spaceURI string) error {
	return s.db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&DefaultSpace{OrgDID: orgDID, SpaceURI: spaceURI}).Error
}

func (s *Store) GetDefaultSpaceURI(orgDID string) (string, error) {
	var row DefaultSpace
	if err := s.db.First(&row, "org_did = ?", orgDID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrNoDefaultSpace
		}
		return "", err
	}
	return row.SpaceURI, nil
}

// GetAnyDefaultSpaceURI returns the space URI for the single configured org.
// Fruitgang is a single-org demo, so there is at most one row.
func (s *Store) GetAnyDefaultSpaceURI() (string, error) {
	var row DefaultSpace
	if err := s.db.First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrNoDefaultSpace
		}
		return "", err
	}
	return row.SpaceURI, nil
}
