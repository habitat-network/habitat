package syncer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/habitat-network/habitat/api/habitat"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// catchUpInterval is how often stale spaces are retried when no notification
// wakes the catch-up loop, e.g. after a failed listRepos.
const catchUpInterval = time.Minute

// spaceSync is how far sap has followed one space's revision sequence. The
// space host stamps every write with a space revision and tells us the one it
// replaced, so a notification whose previous revision isn't Rev means we
// missed one; the space is then marked Stale and caught up with
// listRepos with Rev as the cursor.
type spaceSync struct {
	Space habitat_syntax.SpaceURI `gorm:"primaryKey"`
	// Rev is the newest space revision whose writes we have applied. Empty
	// until the first full listing of the space.
	Rev syntax.TID
	// Seen is the newest space revision any notification has announced, so a
	// catch-up that finishes behind it leaves the space stale instead of
	// declaring itself done.
	Seen  syntax.TID
	Stale bool `gorm:"not null;default:false;index"`
}

// observeSpaceRev applies a notification's space revision pair. A write that
// directly follows the revision we hold advances it; anything else (a gap, or a
// space we have never listed) marks the space stale for runCatchUp. Hosts that
// predate space revisions send none, and are left to the crawl's full listing.
func (e *Engine) observeSpaceRev(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	spaceRev syntax.TID,
	prevSpaceRev syntax.TID,
) error {
	if spaceRev == "" {
		return nil
	}
	stale := false
	err := e.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		stale = false
		var s spaceSync
		err := tx.Where("space = ?", space).First(&s).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// The space's very first write needs no catch-up: there is nothing
			// before it. Any other first sighting does.
			s = spaceSync{Space: space, Seen: spaceRev}
			if prevSpaceRev == "" {
				s.Rev = spaceRev
			} else {
				s.Stale = true
				stale = true
			}
			return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&s).Error
		} else if err != nil {
			return err
		}
		if spaceRev <= s.Rev {
			return nil // duplicate or reordered notification
		}
		updates := map[string]any{}
		if spaceRev > s.Seen {
			updates["seen"] = spaceRev
		}
		if !s.Stale && prevSpaceRev == s.Rev {
			updates["rev"] = spaceRev
		} else if !s.Stale {
			updates["stale"] = true
			stale = true
		} else {
			stale = true
		}
		if len(updates) == 0 {
			return nil
		}
		return tx.Model(&spaceSync{}).Where("space = ?", space).Updates(updates).Error
	})
	if err != nil {
		return fmt.Errorf("observe space rev: %w", err)
	}
	if stale {
		e.catchUp.Notify()
	}
	return nil
}

// RecordSpaceRev notes that every repo in the space has been listed as of
// rev, which comes from a full listRepos. It clears a pending catch-up the
// listing already covers, and never moves the revision backwards.
func (e *Engine) RecordSpaceRev(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	rev syntax.TID,
) error {
	if rev == "" {
		return nil
	}
	return e.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var s spaceSync
		err := tx.Where("space = ?", space).First(&s).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Clauses(clause.OnConflict{DoNothing: true}).
				Create(&spaceSync{Space: space, Rev: rev, Seen: rev}).Error
		} else if err != nil {
			return err
		}
		if rev <= s.Rev {
			return nil
		}
		return tx.Model(&spaceSync{}).Where("space = ?", space).Updates(map[string]any{
			"rev":   rev,
			"seen":  max(s.Seen, rev),
			"stale": s.Seen > rev,
		}).Error
	})
}

func (e *Engine) runCatchUp(ctx context.Context) {
	notify := e.catchUp.Listen()
	ticker := time.NewTicker(catchUpInterval)
	defer ticker.Stop()
	for {
		e.catchUpStale(ctx)
		select {
		case <-ctx.Done():
			return
		case <-notify:
		case <-ticker.C:
		}
	}
}

// catchUpStale catches up every space marked stale.
func (e *Engine) catchUpStale(ctx context.Context) {
	var spaces []habitat_syntax.SpaceURI
	if err := e.db.WithContext(ctx).Model(&spaceSync{}).
		Where("stale = ?", true).Pluck("space", &spaces).Error; err != nil {
		slog.ErrorContext(ctx, "catch-up: list stale spaces", "err", err)
		return
	}
	for _, space := range spaces {
		if err := e.CatchUp(ctx, space); err != nil {
			slog.WarnContext(ctx, "catch-up failed, will retry", "space", space, "err", err)
		}
	}
}

// CatchUp lists the repos written since the last space revision we applied
// (all of them if we hold none), passing it as the listRepos cursor and queues every one that is behind, then
// advances the space revision to the one the listing was taken at. If the host
// rejects the since cursor, it falls back to a full listing.
func (e *Engine) CatchUp(ctx context.Context, space habitat_syntax.SpaceURI) error {
	var s spaceSync
	if err := e.db.WithContext(ctx).Where("space = ?", space).First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	rev, err := e.listSince(ctx, space, s.Rev)
	if err != nil && s.Rev != "" {
		slog.WarnContext(ctx, "catch-up since failed, listing every repo",
			"space", space, "since", s.Rev, "err", err)
		rev, err = e.listSince(ctx, space, "")
	}
	if err != nil {
		return err
	}
	if rev == "" {
		// The host reports no space revision (it predates them): the listing
		// above was a full resync, which is all we can do for it.
		return e.db.WithContext(ctx).Model(&spaceSync{}).
			Where("space = ?", space).Update("stale", false).Error
	}
	return e.RecordSpaceRev(ctx, space, rev)
}

// listSince lists the repos written after since (every repo when empty) and
// observes each one's head. It returns the space revision the listing was
// taken at, which the host reports in the response cursor and reads before any
// repo data, so recording it never skips a write.
func (e *Engine) listSince(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	since syntax.TID,
) (syntax.TID, error) {
	client, err := e.clients.ClientForSpace(ctx, space)
	if err != nil {
		return "", fmt.Errorf("client for space: %w", err)
	}
	params := map[string]any{"space": space.String()}
	if since != "" {
		params["cursor"] = since.String()
	}
	var out habitat.NetworkHabitatSpaceListReposOutput
	if err := client.Get(ctx, "network.habitat.space.listRepos", params, &out); err != nil {
		return "", fmt.Errorf("list repos: %w", err)
	}
	for _, r := range out.Repos {
		// Prefer repoRev, the field the lexicon now requires; fall back to the
		// deprecated rev so hosts that predate the rename still sync.
		rev := r.RepoRev
		if rev == "" {
			rev = r.Rev
		}
		if _, err := e.observeHead(
			ctx, space, syntax.DID(r.Did), syntax.TID(rev), r.Hash,
		); err != nil {
			return "", err
		}
	}
	return syntax.TID(out.Cursor), nil
}
