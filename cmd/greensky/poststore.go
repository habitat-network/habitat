package main

import (
	"context"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// post is the stored representation of a network.habitat.greensky.post record
// that sap has synced. A root post has an empty ReplyParent; a reply carries
// the URIs of the post it answers and the thread's root. Replies always live
// in the same space as their root, so SpaceURI identifies the thread.
type post struct {
	URI         string `gorm:"primaryKey"` // SpaceRecordURI
	SpaceURI    string `gorm:"index"`
	Author      string `gorm:"index"` // DID of the record's authoring repo
	Text        string
	PostedAt    time.Time `gorm:"index"` // client-declared createdAt
	ReplyParent string    // empty for a root post
	ReplyRoot   string
	IndexedAt   time.Time
}

// PostStore persists greensky posts and serves them grouped into threads.
type PostStore struct {
	db *gorm.DB
}

func NewPostStore(db *gorm.DB) (*PostStore, error) {
	if err := db.AutoMigrate(&post{}); err != nil {
		return nil, err
	}
	return &PostStore{db: db}, nil
}

// Upsert inserts or replaces a post by its URI, so re-synced records (e.g. an
// edit) overwrite the previous version rather than duplicating.
func (s *PostStore) Upsert(ctx context.Context, p post) error {
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "uri"}},
			UpdateAll: true,
		}).
		Create(&p).Error
}

// Delete removes a post by its URI, used when sap reports a deletion.
func (s *PostStore) Delete(ctx context.Context, uri string) error {
	return s.db.WithContext(ctx).Where("uri = ?", uri).Delete(&post{}).Error
}

// Thread is a root post together with its replies.
type Thread struct {
	Root    post
	Replies []post
}

// ThreadsForAuthor returns the author's root posts, newest first, each with its
// replies (oldest first). Replies are scoped to the root's space, which is how
// the forum threads them.
func (s *PostStore) ThreadsForAuthor(ctx context.Context, author syntax.DID) ([]Thread, error) {
	var roots []post
	if err := s.db.WithContext(ctx).
		Where("author = ? AND reply_parent = ?", author.String(), "").
		Order("posted_at DESC").
		Find(&roots).Error; err != nil {
		return nil, err
	}

	threads := make([]Thread, 0, len(roots))
	for _, root := range roots {
		var replies []post
		if err := s.db.WithContext(ctx).
			Where("space_uri = ? AND reply_parent <> ?", root.SpaceURI, "").
			Order("posted_at ASC").
			Find(&replies).Error; err != nil {
			return nil, err
		}
		threads = append(threads, Thread{Root: root, Replies: replies})
	}
	return threads, nil
}

// postURI is a small helper to keep the SpaceRecordURI typed where it matters.
func recordAuthor(uri habitat_syntax.SpaceRecordURI) syntax.DID {
	return uri.Repo()
}
