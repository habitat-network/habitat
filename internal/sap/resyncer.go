package sap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"net/url"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/db"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"gorm.io/gorm"
)

var _ db.Store[*resyncer] = (*resyncer)(nil)

type resyncer struct {
	db          *gorm.DB
	orgManager  *orgManager
	repos       *repoManager
	resyncBuf   *resyncBuffer
	parallelism int
}

func (r *resyncer) WithTx(tx *gorm.DB) *resyncer {
	return &resyncer{
		db:          tx,
		orgManager:  r.orgManager,
		repos:       r.repos,
		resyncBuf:   r.resyncBuf,
		parallelism: r.parallelism,
	}
}

func newResyncer(
	db *gorm.DB,
	orgManager *orgManager,
	repos *repoManager,
	resyncBuf *resyncBuffer,
	parallelism int,
) *resyncer {
	if parallelism <= 0 {
		parallelism = 5
	}
	return &resyncer{
		db:          db,
		orgManager:  orgManager,
		repos:       repos,
		resyncBuf:   resyncBuf,
		parallelism: parallelism,
	}
}

func (r *resyncer) run(ctx context.Context) {
	for i := 0; i < r.parallelism; i++ {
		go r.runWorker(ctx, i)
	}
}

func (r *resyncer) runWorker(ctx context.Context, workerID int) {
	logger := slog.Default().With("component", "resyncer", "worker", workerID)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		space, did, found, err := r.claimJob(ctx)
		if err != nil {
			logger.ErrorContext(ctx, "claim resync job", "err", err)
			time.Sleep(time.Second)
			continue
		}
		if !found {
			time.Sleep(time.Second)
			continue
		}

		logger.InfoContext(ctx, "resync repo", "space", space, "repo", did)
		if err := r.syncRepo(ctx, space, did); err != nil {
			logger.ErrorContext(ctx, "resync failed", "space", space, "repo", did, "err", err)
		}
	}
}

func (r *resyncer) claimJob(
	ctx context.Context,
) (habitat_syntax.SpaceURI, syntax.DID, bool, error) {
	for _, state := range []repoState{RepoStatePending, RepoStateDesynced, RepoStateError} {
		space, did, found, err := r.repos.ClaimForResync(ctx, state)
		if err != nil {
			return "", "", false, err
		}
		if found {
			return space, did, true, nil
		}
	}
	return "", "", false, nil
}

func (r *resyncer) syncRepo(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repoDID syntax.DID,
) error {
	orgDID := space.SpaceOwner()
	var org managedOrg
	if err := r.db.WithContext(ctx).First(&org, "did = ?", orgDID).Error; err != nil {
		return fmt.Errorf("load org: %w", err)
	}

	repo, err := r.repos.GetRepo(ctx, space, repoDID)
	if err != nil {
		return err
	}
	since := ""
	if repo != nil && repo.Rev != "" {
		since = repo.Rev.String()
	}

	client := r.orgManager.GetClient(ctx, org.DID)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		params := url.Values{
			"space": []string{space.String()},
			"repo":  []string{repoDID.String()},
		}
		if since != "" {
			params.Set("since", since)
		}
		params.Set("limit", "1000")

		resp, err := client.Get(
			org.Host + "/xrpc/network.habitat.space.getRepoOplog?" + params.Encode(),
		)
		if err != nil {
			return r.handleSyncError(ctx, space, repoDID, err)
		}

		var output habitat.NetworkHabitatSpaceGetRepoOplogOutput
		decodeErr := json.NewDecoder(resp.Body).Decode(&output)
		closeErr := resp.Body.Close()
		if decodeErr != nil {
			return r.handleSyncError(ctx, space, repoDID, decodeErr)
		}
		if closeErr != nil {
			return closeErr
		}
		if resp.StatusCode != http.StatusOK {
			return r.handleSyncError(
				ctx,
				space,
				repoDID,
				fmt.Errorf("getRepoOplog: %s", resp.Status),
			)
		}

		if len(output.Records) > 0 {
			err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				if err := writeOplogRecords(tx, space, repoDID, output.Records); err != nil {
					return err
				}
				lastRev := syntax.TID(output.Records[len(output.Records)-1].Rev)
				return r.repos.WithTx(tx).UpdateRev(ctx, space, repoDID, lastRev)
			})
			if err != nil {
				return r.handleSyncError(ctx, space, repoDID, err)
			}
			if output.Cursor != "" {
				since = output.Cursor
			}
		}

		if output.Cursor == "" || len(output.Records) == 0 {
			break
		}
	}

	if err := r.repos.SetActive(ctx, space, repoDID, syntax.TID(since)); err != nil {
		return r.handleSyncError(ctx, space, repoDID, fmt.Errorf("set active: %w", err))
	}
	if err := r.resyncBuf.drainRepo(ctx, repo); err != nil {
		if markErr := r.repos.MarkDesynced(ctx, space, repoDID); markErr != nil {
			return errors.Join(err, markErr)
		}
		return fmt.Errorf("drain repo after sync: %w", err)
	}
	return nil
}

func (r *resyncer) handleSyncError(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
	syncErr error,
) error {
	repo, err := r.repos.GetRepo(ctx, space, did)
	if err != nil {
		return err
	}
	retryCount := 0
	if repo != nil {
		retryCount = repo.RetryCount + 1
	}
	retryAfter := time.Now().Add(backoff(retryCount, 60))
	errMsg := ""
	if syncErr != nil {
		errMsg = syncErr.Error()
	}
	return r.repos.MarkError(ctx, space, did, errMsg, retryCount, retryAfter)
}

func backoff(retries int, maxMinutes int) time.Duration {
	dur := 1 << retries
	if dur > maxMinutes {
		dur = maxMinutes
	}
	jitter := time.Millisecond * time.Duration(rand.Intn(1000))
	return time.Minute*time.Duration(dur) + jitter
}
