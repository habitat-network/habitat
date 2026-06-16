package sap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/events"
	"github.com/r3labs/sse/v2"
	"gorm.io/gorm"
)

type subscription struct {
	client *sse.Client
	cancel context.CancelFunc
}

type subscriber struct {
	db         *gorm.DB
	orgManager *orgManager

	subscriptionsMu sync.RWMutex
	subscriptions   map[syntax.DID]*subscription
}

func newSubscriber(db *gorm.DB, orgManager *orgManager) *subscriber {
	return &subscriber{
		db:            db,
		orgManager:    orgManager,
		subscriptions: map[syntax.DID]*subscription{},
	}
}

// addSubscription adds a new subscription to subscribeSpaces and tracks it by org id
func (s *subscriber) addSubscription(ctx context.Context, org *managedOrg) {
	client := sse.NewClient(org.Host + "/xrpc/network.habitat.sync.subscribeSpaces")
	client.Connection = s.orgManager.GetClient(ctx, org.DID)
	client.LastEventID.Store([]byte(org.Cursor))
	subscribeCtx, cancel := context.WithCancel(ctx)
	sub := &subscription{
		client: client,
		cancel: cancel,
	}

	err := client.SubscribeRawWithContext(subscribeCtx, func(event *sse.Event) {
		eventType := string(event.Event)
		switch eventType {
		case "space":
			var spaceEvent events.Event
			if err := json.Unmarshal(event.Data, &spaceEvent); err != nil {
				slog.ErrorContext(
					subscribeCtx,
					"failed to unmarshal event",
					"err",
					err,
				)
				return
			}
			err := s.db.Transaction(func(tx *gorm.DB) error {
				if err := tx.Model(&managedOrg{}).
					Where("did = ?", org.DID).
					Update("cursor", spaceEvent.Seq).
					Error; err != nil {
					return err
				}
				var prevRepo managedRepo
				if err := tx.Where("space = ?", spaceEvent.Space).
					Where("did = ?", spaceEvent.Repo).
					First(&prevRepo).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				if prevRepo.State != "" && prevRepo.State != RepoStateActive {
					// not actively listening to events for this repo yet
					// TODO: write to resync buffer
					return nil
				}
				if spaceEvent.Since != "" && prevRepo.Rev != spaceEvent.Since {
					if err := tx.Save(&managedRepo{
						Space: spaceEvent.Space,
						DID:   spaceEvent.Repo,
						State: RepoStateDesynced,
					}).Error; err != nil {
						return err
					}
					slog.WarnContext(
						subscribeCtx,
						"repo desynced",
						"space",
						spaceEvent.Space,
						"repo",
						spaceEvent.Repo,
						"expected",
						spaceEvent.Since,
						"actual",
						prevRepo.Rev,
					)
					return nil
				}
				if err := tx.Save(&managedRepo{
					Space: spaceEvent.Space,
					DID:   spaceEvent.Repo,
					Rev:   spaceEvent.Rev,
					State: RepoStateActive,
				}).Error; err != nil {
					return err
				}
				for _, op := range spaceEvent.Ops {
					value, err := json.Marshal(op.Value)
					if err != nil {
						return err
					}
					if err := tx.Create(&outbox{
						URI:   op.Uri,
						Value: value,
					}).Error; err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				slog.ErrorContext(subscribeCtx, "failed to save space event", "err", err)
				return
			}
		default:
			slog.WarnContext(subscribeCtx, "unknown event type", "event", event.Event)
		}
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed to subscribe", "org", org.DID, "err", err)
		s.db.Model(&managedOrg{}).Where("did = ?", org.DID).UpdateColumn("error_msg", err.Error())
		return
	}

	s.subscriptionsMu.Lock()
	s.subscriptions[org.DID] = sub
	s.subscriptionsMu.Unlock()
}

// closeSubscriptions cleans up the subscriptions
func (s *subscriber) closeSubscriptions() error {
	lastEventIDs := map[syntax.DID]string{}
	s.subscriptionsMu.Lock()
	for did, sub := range s.subscriptions {
		lastEventIDs[did] = string(sub.client.LastEventID.Load().([]byte))
		sub.cancel()
		delete(s.subscriptions, did)
	}
	s.subscriptionsMu.Unlock()
	var errs []error
	for did, cursor := range lastEventIDs {
		// track errors but keep going
		errs = append(errs, s.db.Model(&managedOrg{}).
			Where("did = ?", did).
			UpdateColumn("cursor", cursor).
			Error)
	}
	return errors.Join(errs...)
}

// loadSubscriptions loads orgs from the database and adds them to the subscriptions
func (s *subscriber) loadSubscriptions(ctx context.Context) error {
	activeSubs := []syntax.DID{}
	s.subscriptionsMu.RLock()
	for k := range s.subscriptions {
		activeSubs = append(activeSubs, k)
	}
	s.subscriptionsMu.RUnlock()
	var orgs []managedOrg
	query := s.db.Where("access_token != ''")
	if len(activeSubs) > 0 {
		query = query.Where("did NOT IN (?)", activeSubs)
	}
	err := query.Find(&orgs).Error
	if err != nil {
		return fmt.Errorf("load subscriptions: %w", err)
	}
	for _, o := range orgs {
		go s.addSubscription(ctx, &o)
	}
	slog.InfoContext(ctx, "loaded subscriptions", "count", len(orgs))
	return nil
}
