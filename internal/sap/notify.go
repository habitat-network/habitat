package sap

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/httpx"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

var (
	nsidNotifyWrite        = syntax.NSID("network.habitat.space.notifyWrite")
	nsidNotifySpaceDeleted = syntax.NSID("network.habitat.space.notifySpaceDeleted")
)

// serviceAuthValidator validates an atproto service-auth bearer token for a
// given lexicon method and returns the issuer DID. *auth.ServiceAuthValidator
// satisfies it.
type serviceAuthValidator interface {
	Validate(ctx context.Context, token string, lxm *syntax.NSID) (syntax.DID, error)
}

// notifyServer exposes the syncer-side entry points for the space host's push
// notifications: notifyWrite (a repo advanced) drives an incremental resync, and
// notifySpaceDeleted drops the syncer's tracking for the space. Both are
// authenticated with atproto service auth, per the permissioned-data sync
// proposal.
type notifyServer struct {
	db       *gorm.DB
	resyncer *resyncer
	auth     serviceAuthValidator
}

func newNotifyServer(db *gorm.DB, r *resyncer, auth serviceAuthValidator) *notifyServer {
	return &notifyServer{db: db, resyncer: r, auth: auth}
}

// HandleNotifyWrite handles network.habitat.space.notifyWrite: the space host
// tells the syncer that a repo has advanced to a new revision. The syncer
// schedules an incremental sync of that repo, which re-verifies its hash.
func (n *notifyServer) HandleNotifyWrite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSpaceNotifyWriteInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode notifyWrite", err)
		return
	}
	space, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space")
	if !ok {
		return
	}
	repo, ok := httpx.ParseDIDInput(ctx, w, input.Repo, "repo")
	if !ok {
		return
	}
	if !n.authorize(w, r, nsidNotifyWrite, space) {
		return
	}

	if err := n.resyncer.enqueueRepo(ctx, space, repo, syntax.TID(input.Rev)); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("enqueue repo: %w", err))
		return
	}
	slog.InfoContext(ctx, "notifyWrite queued resync", "space", space, "repo", repo, "rev", input.Rev)
	w.WriteHeader(http.StatusOK)
}

// HandleNotifySpaceDeleted handles network.habitat.space.notifySpaceDeleted: the
// space authority tells the syncer a space is gone. The syncer stops tracking
// it by dropping its repos, associations, and buffered events.
func (n *notifyServer) HandleNotifySpaceDeleted(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSpaceNotifySpaceDeletedInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode notifySpaceDeleted", err)
		return
	}
	space, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space")
	if !ok {
		return
	}
	if !n.authorize(w, r, nsidNotifySpaceDeleted, space) {
		return
	}

	if err := n.forgetSpace(ctx, space); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("forget space: %w", err))
		return
	}
	slog.InfoContext(ctx, "notifySpaceDeleted dropped tracking", "space", space)
	w.WriteHeader(http.StatusOK)
}

// authorize validates the request's service-auth token for lxm and checks that
// its issuer is the space's authority (its owner DID), which is who signs notify
// events. On failure it writes the HTTP error and returns false.
func (n *notifyServer) authorize(
	w http.ResponseWriter,
	r *http.Request,
	lxm syntax.NSID,
	space habitat_syntax.SpaceURI,
) bool {
	ctx := r.Context()
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		httpx.WriteInvalidRequest(ctx, w, "missing bearer token", fmt.Errorf("no service auth"))
		return false
	}
	issuer, err := n.auth.Validate(ctx, token, &lxm)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return false
	}
	if issuer != space.SpaceOwner() {
		httpx.WriteInvalidRequest(ctx, w, "not the space authority",
			fmt.Errorf("issuer %q is not the authority for %q", issuer, space))
		return false
	}
	return true
}

// forgetSpace removes all of the syncer's tracking state for a deleted space.
func (n *notifyServer) forgetSpace(ctx context.Context, space habitat_syntax.SpaceURI) error {
	return n.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("space = ?", space).Delete(&managedRepo{}).Error; err != nil {
			return err
		}
		if err := tx.Where("space = ?", space).Delete(&managedSpace{}).Error; err != nil {
			return err
		}
		return tx.Where("space = ?", space).Delete(&bufferedEvent{}).Error
	})
}
