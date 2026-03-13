package clique

import (
	"errors"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bradenaw/juniper/xslices"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// This package manages cliques, a way to delegate permissions to other users and have shared group experiences.
type Ref string

// cliqueMember is the GORM model. Fields must be exported for GORM to read/write them.
type cliqueMember struct {
	Owner  string `gorm:"primaryKey"`
	Key    string `gorm:"primaryKey"`
	Member string `gorm:"primaryKey"`
	// scopes []string // TODO: add this later

	// Automatically populated by gorm: https://gorm.io/docs/models.html#Declaring-Models
	CreatedAt time.Time
}

type Store interface {
	CreateClique(owner syntax.DID, members []syntax.DID) (Ref, error)
	GetMembers(owner syntax.DID, key string) ([]syntax.DID, error)
	AddMember(owner syntax.DID, key string, member syntax.DID) error
	RemoveMember(owner syntax.DID, key string, member syntax.DID) error
	IsMember(owner syntax.DID, key string, maybeMember syntax.DID) (bool, error)
}

type store struct {
	db *gorm.DB
}

var _ Store = &store{}

func NewStore(db *gorm.DB) (*store, error) {
	err := db.AutoMigrate(&cliqueMember{})
	if err != nil {
		return nil, fmt.Errorf("failed to migrate clique_members table: %w", err)
	}
	return &store{db: db}, nil
}

var (
	ErrCliqueNotFound     = errors.New("no matching clique found")
	ErrSelfIsAlwaysMember = errors.New("self is always member of a clique")
)

// CreateClique creates a clique owned by owner, with members, and returns the key of this clique.
func (s *store) CreateClique(owner syntax.DID, members []syntax.DID) (Ref, error) {
	key := uuid.New().String()
	rows := make([]cliqueMember, len(members))

	for i, m := range members {
		rows[i] = cliqueMember{
			Owner:  owner.String(),
			Key:    key,
			Member: m.String(),
		}
	}

	// Owner is always in the clique -- this signifies clique existence for an empty clique
	rows = append(rows, cliqueMember{
		Owner:  owner.String(),
		Key:    key,
		Member: owner.String(),
	})

	err := s.db.Create(&rows).Error
	if err != nil {
		return "", err
	}

	return Ref(fmt.Sprintf("%s:%s", owner, key)), nil
}

// GetMembers returns all members of the clique identified by (owner, key).
func (s *store) GetMembers(owner syntax.DID, key string) ([]syntax.DID, error) {
	var members []string
	err := s.db.Model(cliqueMember{}).Where("owner = ? AND key = ?", owner.String(), key).Pluck("member", &members).Error
	if err != nil {
		return nil, err
	}

	return xslices.Map(members, func(m string) syntax.DID {
		return syntax.DID(m)
	}), nil
}

// AddMember adds a member to an existing clique. No-ops if already a member.
func (s *store) AddMember(owner syntax.DID, key string, member syntax.DID) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		// First check existence of the clique
		var ownerMembership cliqueMember
		err := tx.Where("owner = ? AND key = ? AND member = ?", owner, key, owner).First(&ownerMembership).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrCliqueNotFound
		} else if err != nil {
			return err
		}

		// If that passes, try creating the clique member (no-op if exists already)
		return tx.FirstOrCreate(&cliqueMember{
			Owner:  owner.String(),
			Key:    key,
			Member: member.String(),
		}).Error
	})
}

// IsMember returns true if maybeMember is in the clique identified by (owner, key).
// The owner is always considered a member of their own cliques.
func (s *store) IsMember(owner syntax.DID, key string, maybeMember syntax.DID) (bool, error) {
	var row cliqueMember
	err := s.db.
		Where("owner = ? AND key = ? AND member = ?", owner.String(), key, maybeMember.String()).
		First(&row).
		Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}

	return true, nil
}

// RemoveMember implements Store.
func (s *store) RemoveMember(owner syntax.DID, key string, member syntax.DID) error {
	if owner == member {
		return ErrSelfIsAlwaysMember
	}
	return s.db.Where("owner = ? AND key = ? AND member = ?").Delete(&cliqueMember{}).Error
}
