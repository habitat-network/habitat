package userstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
)

// UserStore is an interface for managing users in the database.
type UserStore interface {
	// EnsureUser ensures that a user with the given DID exists in the users table.
	// If the user doesn't exist, it inserts them. If they already exist, it does nothing.
	EnsureUser(did syntax.DID) error

	// CheckUserExists checks if a user with the given DID exists in the users table.
	CheckUserExists(did syntax.DID) (bool, error)
}

// User represents a user in the database
type User struct {
	Did string `gorm:"primaryKey;uniqueIndex"`
}

// NewUserStore creates a new UserStore implementation.
func NewUserStore(db *gorm.DB) (UserStore, error) {
	// Run migrations
	if err := db.AutoMigrate(&User{}); err != nil {
		return nil, fmt.Errorf("failed to migrate users table: %w", err)
	}

	return &userStore{
		db: db,
	}, nil
}

type userStore struct {
	db *gorm.DB
}

var _ UserStore = (*userStore)(nil)

// EnsureUser implements [UserStore].
func (u *userStore) EnsureUser(did syntax.DID) error {
	user := User{Did: did.String()}
	// Check if user exists
	_, err := gorm.G[User](u.db).Where("did = ?", did.String()).First(context.Background())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// User doesn't exist, create it
		err = gorm.G[User](u.db).Create(context.Background(), &user)
		return err
	} else if err != nil {
		return err
	}
	// User already exists, nothing to do
	return nil
}

// CheckUserExists implements [UserStore].
func (u *userStore) CheckUserExists(did syntax.DID) (bool, error) {
	_, err := gorm.G[User](u.db).Where("did = ?", did.String()).First(context.Background())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}
