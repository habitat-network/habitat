package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/sync"
)

type Syncer struct {
	client *PearClient
	db     *gorm.DB
	cfg    *Config
	log    zerolog.Logger
}

func NewSyncer(client *PearClient, db *gorm.DB, cfg *Config, log zerolog.Logger) *Syncer {
	return &Syncer{
		client: client,
		db:     db,
		cfg:    cfg,
		log:    log,
	}
}

func (s *Syncer) Run(ctx context.Context) error {
	s.log.Info().Msg("starting sync")

	if err := s.initialSync(ctx); err != nil {
		return fmt.Errorf("initial sync: %w", err)
	}

	go func() {
		for {
			if err := s.handleLiveEvents(ctx); err != nil {
				s.log.Warn().Err(err).Msg("live event handler error, reconnecting in 5s")
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
	s.log.Info().Msg("sync stopped")
	return nil
}

func (s *Syncer) initialSync(ctx context.Context) error {
	s.log.Info().Msg("initial sync started")

	spaces, err := s.client.ListSpaces(ctx, s.cfg.SpaceTypes)
	if err != nil {
		return fmt.Errorf("list spaces: %w", err)
	}

	s.log.Info().Int("count", len(spaces)).Msg("discovered spaces")

	for _, sp := range spaces {
		space := getString(sp, "space")
		if space == "" {
			continue
		}

		s.log.Debug().Str("space", space).Msg("syncing space")

		state, err := s.client.GetSpaceState(ctx, space)
		if err != nil {
			s.log.Warn().Err(err).Str("space", space).Msg("get space state failed")
			continue
		}
		spaceRev := getString(state, "spaceRev")
		memberRev := getString(state, "memberRev")

		if err := s.backfillRecords(ctx, space, s.db); err != nil {
			s.log.Warn().Err(err).Str("space", space).Msg("backfill records failed")
		}

		if err := s.backfillMembers(ctx, space, s.db); err != nil {
			s.log.Warn().Err(err).Str("space", space).Msg("backfill members failed")
		}

		spaceType := getString(sp, "spaceType")
		org := extractOrg(space)
		if err := s.db.Where("space = ?", space).Delete(&SpaceState{}).Error; err != nil {
			s.log.Warn().Err(err).Str("space", space).Msg("delete space state failed")
			continue
		}
		if err := s.db.Create(&SpaceState{
			Org:       org,
			Space:     space,
			SpaceType: spaceType,
			State:     "active",
			SpaceRev:  spaceRev,
			MemberRev: memberRev,
		}).Error; err != nil {
			s.log.Warn().Err(err).Str("space", space).Msg("create space state failed")
		}
	}

	s.log.Info().Msg("initial sync complete")
	return nil
}

func (s *Syncer) backfillRecords(ctx context.Context, space string, db *gorm.DB) error {
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
			db.Where("space = ? AND repo = ? AND collection = ? AND rkey = ?",
				space, getString(rec, "repo"), getString(rec, "collection"), getString(rec, "rkey")).
				Delete(&RecordState{})

			valBytes, _ := json.Marshal(rec["value"])
			db.Create(&RecordState{
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

func (s *Syncer) backfillMembers(ctx context.Context, space string, db *gorm.DB) error {
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
				db.Where("space = ? AND did = ?", space, did).Delete(&MemberState{})
				db.Create(&MemberState{
					Space:  space,
					DID:    did,
					Access: access,
					Rev:    getString(op, "rev"),
				})
			} else if action == "remove" {
				db.Where("space = ? AND did = ?", space, did).Delete(&MemberState{})
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
	var lastSeq int64
	s.db.Raw("SELECT COALESCE(MAX(last_seq), 0) FROM space_states").Scan(&lastSeq)

	stream, err := s.client.SubscribeSpaces(ctx, lastSeq, s.cfg.SpaceTypes)
	if err != nil {
		return fmt.Errorf("subscribe spaces: %w", err)
	}
	defer stream.Close()

	s.log.Info().Int64("cursor", lastSeq).Msg("connected to live event stream")

	for {
		ev, err := stream.ReadEvent()
		if err != nil {
			return fmt.Errorf("read event: %w", err)
		}
		if err := s.applyEvent(ev); err != nil {
			s.log.Warn().Err(err).Msg("failed to apply event")
		}
	}
}

func (s *Syncer) applyEvent(ev sync.Event) (err error) {
	tx := s.db.Begin()
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	space := ev.Space

	switch ev.Type {
	case sync.EventSpaceRecord:
		action := ev.Action
		if action == "delete" {
			if err := tx.Where("space = ? AND repo = ? AND collection = ? AND rkey = ?",
				space, ev.Repo, ev.Collection, ev.Rkey).Delete(&RecordState{}).Error; err != nil {
				return fmt.Errorf("delete record: %w", err)
			}
		} else {
			recBytes, jsonErr := json.Marshal(ev.Record)
			if jsonErr != nil {
				return fmt.Errorf("marshal record: %w", jsonErr)
			}
			if err := tx.Where("space = ? AND repo = ? AND collection = ? AND rkey = ?",
				space, ev.Repo, ev.Collection, ev.Rkey).Delete(&RecordState{}).Error; err != nil {
				return fmt.Errorf("delete before upsert: %w", err)
			}
			if err := tx.Create(&RecordState{
				Space:      space,
				Repo:       ev.Repo,
				Collection: ev.Collection,
				Rkey:       ev.Rkey,
				Rev:        ev.Rev,
				Record:     recBytes,
			}).Error; err != nil {
				return fmt.Errorf("upsert record: %w", err)
			}
		}

	case sync.EventSpaceMember:
		action := ev.Action
		if action == "add" {
			if err := tx.Where("space = ? AND did = ?", space, ev.DID).Delete(&MemberState{}).Error; err != nil {
				return fmt.Errorf("delete member before upsert: %w", err)
			}
			if err := tx.Create(&MemberState{Space: space, DID: ev.DID, Access: ev.Access, Rev: ev.Rev}).Error; err != nil {
				return fmt.Errorf("upsert member: %w", err)
			}
		} else {
			if err := tx.Where("space = ? AND did = ?", space, ev.DID).Delete(&MemberState{}).Error; err != nil {
				return fmt.Errorf("delete member: %w", err)
			}
		}

	case sync.EventSpace:
		if ev.Action == "delete" {
			for _, model := range []interface{}{&SpaceState{}, &RepoState{}, &RecordState{}, &MemberState{}} {
				if err := tx.Where("space = ?", space).Delete(model).Error; err != nil {
					return fmt.Errorf("delete space data: %w", err)
				}
			}
		}
	case sync.EventIdentity:
		// informational only
	}

	evJSON, jsonErr := json.Marshal(ev)
	if jsonErr != nil {
		return fmt.Errorf("marshal event: %w", jsonErr)
	}
	if err := tx.Create(&OutboxEvent{
		EventJSON: string(evJSON),
		Acked:     s.cfg.DisableAcks,
	}).Error; err != nil {
		return fmt.Errorf("create outbox event: %w", err)
	}

	if err := tx.Model(&SpaceState{}).Where("space = ?", space).Updates(map[string]interface{}{
		"last_seq":   ev.Seq,
		"last_rev":   ev.Rev,
		"updated_at": time.Now(),
	}).Error; err != nil {
		return fmt.Errorf("update space state: %w", err)
	}

	return tx.Commit().Error
}

func (s *Syncer) reconcile(ctx context.Context) {
	s.log.Debug().Msg("periodic reconciliation")

	spaces, err := s.client.ListSpaces(ctx, s.cfg.SpaceTypes)
	if err != nil {
		s.log.Warn().Err(err).Msg("reconciliation: list spaces failed")
		return
	}

	sem := make(chan struct{}, s.cfg.ResyncParallelism)

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
			s.log.Warn().
				Str("space", space).
				Str("local", local.SpaceRev).
				Str("remote", remoteRev).
				Msg("space rev mismatch, triggering resync")
			sem <- struct{}{}
			go func(sp string) {
				defer func() { <-sem }()
				s.resyncSpace(ctx, sp)
			}(space)
		}
	}
}

func (s *Syncer) resyncSpace(ctx context.Context, space string) {
	if err := s.db.Model(&SpaceState{}).Where("space = ?", space).
		Update("state", "resyncing").Error; err != nil {
		s.log.Warn().Err(err).Str("space", space).Msg("resync: set state failed")
		return
	}

	state, err := s.client.GetSpaceState(ctx, space)
	if err != nil {
		s.log.Warn().Err(err).Str("space", space).Msg("resync: get state failed")
		return
	}
	remoteSpaceRev := getString(state, "spaceRev")
	remoteMemberRev := getString(state, "memberRev")

	var local SpaceState
	if err := s.db.Where("space = ?", space).First(&local).Error; err != nil {
		s.log.Warn().Err(err).Str("space", space).Msg("resync: local state not found")
		return
	}

	txErr := s.db.Transaction(func(tx *gorm.DB) error {
		if local.SpaceRev != "" {
			changes, _, err := s.client.ListRecordChanges(ctx, space, "", local.SpaceRev, 1000)
			if err != nil {
				s.log.Warn().Err(err).Str("space", space).
					Msg("resync: incremental records failed, full backfill")
				if err := s.backfillRecords(ctx, space, tx); err != nil {
					return fmt.Errorf("backfill records: %w", err)
				}
			} else {
				for _, ch := range changes {
					action := getString(ch, "action")
					repo := getString(ch, "repo")
					collection := getString(ch, "collection")
					rkey := getString(ch, "rkey")
					if action == "delete" {
						if err := tx.Where("space = ? AND repo = ? AND collection = ? AND rkey = ?",
							space, repo, collection, rkey).Delete(&RecordState{}).Error; err != nil {
							return fmt.Errorf("delete record: %w", err)
						}
					} else {
						valBytes, jsonErr := json.Marshal(ch["value"])
						if jsonErr != nil {
							return fmt.Errorf("marshal record value: %w", jsonErr)
						}
						if err := tx.Where("space = ? AND repo = ? AND collection = ? AND rkey = ?",
							space, repo, collection, rkey).Delete(&RecordState{}).Error; err != nil {
							return fmt.Errorf("delete before upsert: %w", err)
						}
						if err := tx.Create(&RecordState{
							Space: space, Repo: repo, Collection: collection, Rkey: rkey,
							Rev: getString(ch, "rev"), Record: valBytes,
						}).Error; err != nil {
							return fmt.Errorf("upsert record: %w", err)
						}
					}
				}
			}
		}

		if local.MemberRev != "" {
			ops, _, err := s.client.GetMemberOplog(ctx, space, local.MemberRev, 1000)
			if err != nil {
				s.log.Warn().Err(err).Str("space", space).
					Msg("resync: get member oplog failed")
			} else {
				for _, op := range ops {
					did := getString(op, "did")
					action := getString(op, "action")
					if action == "add" {
						if err := tx.Where("space = ? AND did = ?", space, did).Delete(&MemberState{}).Error; err != nil {
							return fmt.Errorf("delete member before upsert: %w", err)
						}
						if err := tx.Create(&MemberState{
							Space: space, DID: did, Access: getString(op, "access"),
							Rev: getString(op, "rev"),
						}).Error; err != nil {
							return fmt.Errorf("upsert member: %w", err)
						}
					} else {
						if err := tx.Where("space = ? AND did = ?", space, did).Delete(&MemberState{}).Error; err != nil {
							return fmt.Errorf("delete member: %w", err)
						}
					}
				}
			}
		}

		if err := tx.Model(&SpaceState{}).Where("space = ?", space).Updates(map[string]interface{}{
			"space_rev": remoteSpaceRev, "member_rev": remoteMemberRev, "state": "active",
		}).Error; err != nil {
			return fmt.Errorf("update space state: %w", err)
		}

		return nil
	})
	if txErr != nil {
		s.log.Warn().Err(txErr).Str("space", space).Msg("resync: transaction failed")
	}
}
