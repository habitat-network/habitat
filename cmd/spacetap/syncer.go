package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"
)

type Syncer struct {
	client *PearClient
	db     *gorm.DB
	cfg    *Config
}

func NewSyncer(client *PearClient, db *gorm.DB, cfg *Config) *Syncer {
	return &Syncer{
		client: client,
		db:     db,
		cfg:    cfg,
	}
}

func (s *Syncer) Run(ctx context.Context) error {
	slog.Info("starting sync")

	if err := s.initialSync(ctx); err != nil {
		return fmt.Errorf("initial sync: %w", err)
	}

	go func() {
		for {
			if err := s.handleLiveEvents(ctx); err != nil {
				slog.Warn("live event handler error, reconnecting in 5s", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.reconcile(ctx)
			}
		}
	}()

	<-ctx.Done()
	slog.Info("sync stopped")
	return nil
}

func (s *Syncer) initialSync(ctx context.Context) error {
	slog.Info("initial sync started")

	spaces, err := s.client.ListSpaces(ctx, s.cfg.SpaceTypes)
	if err != nil {
		return fmt.Errorf("list spaces: %w", err)
	}

	slog.Info("discovered spaces", "count", len(spaces))

	for _, sp := range spaces {
		space := getString(sp, "space")
		if space == "" {
			continue
		}

		slog.Debug("syncing space", "space", space)

		state, err := s.client.GetSpaceState(ctx, space)
		if err != nil {
			slog.Warn("get space state failed", "err", err, "space", space)
			continue
		}
		spaceRev := getString(state, "spaceRev")
		memberRev := getString(state, "memberRev")

		if err := s.backfillRecords(ctx, space); err != nil {
			slog.Warn("backfill records failed", "err", err, "space", space)
		}

		if err := s.backfillMembers(ctx, space); err != nil {
			slog.Warn("backfill members failed", "err", err, "space", space)
		}

		spaceType := getString(sp, "spaceType")
		org := extractOrg(space)
		s.db.Where("space = ?", space).Delete(&SpaceState{})
		s.db.Create(&SpaceState{
			Org:       org,
			Space:     space,
			SpaceType: spaceType,
			State:     "active",
			SpaceRev:  spaceRev,
			MemberRev: memberRev,
		})
	}

	slog.Info("initial sync complete")
	return nil
}

func (s *Syncer) backfillRecords(ctx context.Context, space string) error {
	var cursor string
	for {
		records, nextCursor, err := s.client.ListRecords(ctx, space, "", cursor, 100)
		if err != nil {
			return fmt.Errorf("list records: %w", err)
		}
		if len(records) == 0 {
			break
		}
		for _, rec := range records {
			s.db.Where("space = ? AND repo = ? AND collection = ? AND rkey = ?",
				space, getString(rec, "repo"), getString(rec, "collection"), getString(rec, "rkey")).
				Delete(&RecordState{})

			valBytes, _ := json.Marshal(rec["value"])
			s.db.Create(&RecordState{
				Space:      space,
				Repo:       getString(rec, "repo"),
				Collection: getString(rec, "collection"),
				Rkey:       getString(rec, "rkey"),
				Rev:        getString(rec, "rev"),
				Record:     valBytes,
			})
		}
		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}
	return nil
}

func (s *Syncer) backfillMembers(ctx context.Context, space string) error {
	var since string
	for {
		ops, nextCursor, err := s.client.GetMemberOplog(ctx, space, since, 100)
		if err != nil {
			return fmt.Errorf("get member oplog: %w", err)
		}
		if len(ops) == 0 {
			break
		}
		for _, op := range ops {
			did := getString(op, "did")
			action := getString(op, "action")
			access := getString(op, "access")

			if action == "add" {
				s.db.Where("space = ? AND did = ?", space, did).Delete(&MemberState{})
				s.db.Create(&MemberState{
					Space:  space,
					DID:    did,
					Access: access,
					Rev:    getString(op, "rev"),
				})
			} else if action == "remove" {
				s.db.Where("space = ? AND did = ?", space, did).Delete(&MemberState{})
			}
		}
		if nextCursor == "" {
			break
		}
		since = nextCursor
	}
	return nil
}

func (s *Syncer) handleLiveEvents(ctx context.Context) error {
	var lastRev string
	s.db.Raw("SELECT COALESCE(MAX(last_rev), '') FROM space_states").Scan(&lastRev)

	conn, err := s.client.SubscribeSpaces(ctx, lastRev, s.cfg.SpaceTypes)
	if err != nil {
		return fmt.Errorf("subscribe spaces: %w", err)
	}
	defer conn.Close()

	slog.Info("connected to live event stream", "cursor", lastRev)

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read message: %w", err)
		}

		var ev map[string]interface{}
		if err := json.Unmarshal(message, &ev); err != nil {
			slog.Warn("failed to unmarshal event", "err", err)
			continue
		}

		if err := s.applyEvent(ev); err != nil {
			slog.Warn("failed to apply event", "err", err)
		}
	}
}

func (s *Syncer) applyEvent(ev map[string]interface{}) error {
	eventType := getString(ev, "type")
	space := getString(ev, "space")
	rev := getString(ev, "rev")

	switch eventType {
	case "space_record":
		action := getString(ev, "action")
		repo := getString(ev, "repo")
		collection := getString(ev, "collection")
		rkey := getString(ev, "rkey")

		if action == "delete" {
			s.db.Where("space = ? AND repo = ? AND collection = ? AND rkey = ?",
				space, repo, collection, rkey).Delete(&RecordState{})
		} else {
			recBytes, _ := json.Marshal(ev["record"])
			s.db.Where("space = ? AND repo = ? AND collection = ? AND rkey = ?",
				space, repo, collection, rkey).Delete(&RecordState{})
			s.db.Create(&RecordState{
				Space:      space,
				Repo:       repo,
				Collection: collection,
				Rkey:       rkey,
				Rev:        rev,
				Record:     recBytes,
			})
		}

	case "space_member":
		action := getString(ev, "action")
		did := getString(ev, "did")
		access := getString(ev, "access")

		if action == "add" {
			s.db.Where("space = ? AND did = ?", space, did).Delete(&MemberState{})
			s.db.Create(&MemberState{Space: space, DID: did, Access: access, Rev: rev})
		} else {
			s.db.Where("space = ? AND did = ?", space, did).Delete(&MemberState{})
		}

	case "space":
		action := getString(ev, "action")
		if action == "delete" {
			s.db.Where("space = ?", space).Delete(&SpaceState{})
			s.db.Where("space = ?", space).Delete(&RepoState{})
			s.db.Where("space = ?", space).Delete(&RecordState{})
			s.db.Where("space = ?", space).Delete(&MemberState{})
		}
	}

	evJSON, _ := json.Marshal(ev)
	s.db.Create(&OutboxEvent{
		EventJSON: string(evJSON),
		Acked:     s.cfg.DisableAcks,
	})

	s.db.Model(&SpaceState{}).Where("space = ?", space).Updates(map[string]interface{}{
		"last_rev":   rev,
		"updated_at": time.Now(),
	})

	return nil
}

func (s *Syncer) reconcile(ctx context.Context) {
	slog.Debug("periodic reconciliation")

	spaces, err := s.client.ListSpaces(ctx, s.cfg.SpaceTypes)
	if err != nil {
		slog.Warn("reconciliation: list spaces failed", "err", err)
		return
	}

	for _, sp := range spaces {
		space := getString(sp, "space")
		if space == "" {
			continue
		}

		state, err := s.client.GetSpaceState(ctx, space)
		if err != nil {
			continue
		}
		remoteRev := getString(state, "spaceRev")

		var local SpaceState
		if err := s.db.Where("space = ?", space).First(&local).Error; err != nil {
			continue
		}

		if local.SpaceRev != remoteRev {
			slog.Warn("space rev mismatch, triggering resync",
				"space", space, "local", local.SpaceRev, "remote", remoteRev)
			go s.resyncSpace(ctx, space)
		}
	}
}

func (s *Syncer) resyncSpace(ctx context.Context, space string) {
	s.db.Model(&SpaceState{}).Where("space = ?", space).Update("state", "resyncing")

	state, err := s.client.GetSpaceState(ctx, space)
	if err != nil {
		slog.Warn("resync: get state failed", "err", err, "space", space)
		return
	}

	var local SpaceState
	s.db.Where("space = ?", space).First(&local)

	if local.SpaceRev != "" {
		changes, _, err := s.client.ListRecordChanges(ctx, space, "", local.SpaceRev, 1000)
		if err != nil {
			s.backfillRecords(ctx, space)
		} else {
			for _, ch := range changes {
				action := getString(ch, "action")
				repo := getString(ch, "repo")
				collection := getString(ch, "collection")
				rkey := getString(ch, "rkey")
				if action == "delete" {
					s.db.Where("space = ? AND repo = ? AND collection = ? AND rkey = ?",
						space, repo, collection, rkey).Delete(&RecordState{})
				} else {
					valBytes, _ := json.Marshal(ch["value"])
					s.db.Where("space = ? AND repo = ? AND collection = ? AND rkey = ?",
						space, repo, collection, rkey).Delete(&RecordState{})
					s.db.Create(&RecordState{
						Space: space, Repo: repo, Collection: collection, Rkey: rkey,
						Rev: getString(ch, "rev"), Record: valBytes,
					})
				}
			}
		}
	}

	if local.MemberRev != "" {
		ops, _, err := s.client.GetMemberOplog(ctx, space, local.MemberRev, 1000)
		if err == nil {
			for _, op := range ops {
				did := getString(op, "did")
				action := getString(op, "action")
				if action == "add" {
					s.db.Where("space = ? AND did = ?", space, did).Delete(&MemberState{})
					s.db.Create(&MemberState{
						Space: space, DID: did, Access: getString(op, "access"),
						Rev: getString(op, "rev"),
					})
				} else {
					s.db.Where("space = ? AND did = ?", space, did).Delete(&MemberState{})
				}
			}
		}
	}

	spaceRev := getString(state, "spaceRev")
	memberRev := getString(state, "memberRev")
	s.db.Model(&SpaceState{}).Where("space = ?", space).Updates(map[string]interface{}{
		"space_rev": spaceRev, "member_rev": memberRev, "state": "active",
	})
}
