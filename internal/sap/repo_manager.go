package sap

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/habitat-network/habitat/internal/db"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

type repoManager struct {
	db *gorm.DB
}

var _ db.Store[*repoManager] = (*repoManager)(nil)

func newRepoManager(db *gorm.DB) *repoManager {
	return &repoManager{db: db}
}

func (rm *repoManager) WithTx(tx *gorm.DB) *repoManager {
	return &repoManager{db: tx}
}

func (rm *repoManager) GetRepo(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
) (*managedRepo, error) {
	var repo managedRepo
	err := rm.db.WithContext(ctx).
		Where("space = ? AND did = ?", space, did).
		First(&repo).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

func (rm *repoManager) EnsureRepo(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
) error {
	return rm.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&managedRepo{
			Space: space,
			DID:   did,
			State: RepoStatePending,
		}).Error
}

func (rm *repoManager) ClaimForResync(
	ctx context.Context,
	state repoState,
) (habitat_syntax.SpaceURI, syntax.DID, bool, error) {
	var space string
	var did string
	now := time.Now().Unix()
	row := rm.db.WithContext(ctx).Raw(`
		UPDATE managed_repos SET state = ?
		WHERE rowid = (
			SELECT rowid FROM managed_repos
			WHERE state = ? AND (retry_after = 0 OR retry_after < ?)
			LIMIT 1
		)
		RETURNING space, did
	`, RepoStateResyncing, state, now).Row()
	err := row.Scan(&space, &did)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return habitat_syntax.SpaceURI(space), syntax.DID(did), true, nil
}

func (rm *repoManager) SetActive(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
	rev syntax.TID,
) error {
	return rm.db.WithContext(ctx).
		Model(&managedRepo{}).
		Where("space = ? AND did = ?", space, did).
		Updates(map[string]any{
			"state":       RepoStateActive,
			"rev":         rev,
			"error_msg":   "",
			"retry_count": 0,
			"retry_after": 0,
		}).Error
}

func (rm *repoManager) UpdateRev(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
	rev syntax.TID,
) error {
	return rm.db.WithContext(ctx).
		Model(&managedRepo{}).
		Where("space = ? AND did = ?", space, did).
		Update("rev", rev).Error
}

func (rm *repoManager) MarkDesynced(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
) error {
	return rm.db.WithContext(ctx).
		Model(&managedRepo{}).
		Where("space = ? AND did = ?", space, did).
		Update("state", RepoStateDesynced).Error
}

func (rm *repoManager) MarkError(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
	errMsg string,
	retryCount int,
	retryAfter time.Time,
) error {
	return rm.db.WithContext(ctx).
		Model(&managedRepo{}).
		Where("space = ? AND did = ?", space, did).
		Updates(map[string]any{
			"state":       RepoStateError,
			"error_msg":   errMsg,
			"retry_count": retryCount,
			"retry_after": retryAfter.Unix(),
		}).Error
}

func (rm *repoManager) ResetPartiallyResynced(ctx context.Context) error {
	return rm.db.WithContext(ctx).
		Model(&managedRepo{}).
		Where("state = ?", RepoStateResyncing).
		Update("state", RepoStateDesynced).Error
}
