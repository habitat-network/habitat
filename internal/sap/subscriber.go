package sap

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"sync"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/events"
	"github.com/r3labs/sse/v2"
	"gorm.io/gorm"
)

type subscription struct {
	client *sse.Client
	cancel context.CancelFunc
}

var _ db.Store[*subscriber] = (*subscriber)(nil)

type subscriber struct {
	db         *gorm.DB
	orgManager *orgManager
	resyncBuf  *resyncBuffer

	subscriptionsMu sync.RWMutex
	subscriptions   map[syntax.DID]*subscription
}

func (s *subscriber) WithTx(tx *gorm.DB) *subscriber {
	return &subscriber{
		db:            tx,
		orgManager:    s.orgManager,
		resyncBuf:     s.resyncBuf,
		subscriptions: s.subscriptions,
	}
}

func newSubscriber(
	db *gorm.DB,
	orgManager *orgManager,
	resyncBuf *resyncBuffer,
) *subscriber {
	return &subscriber{
		db:            db,
		orgManager:    orgManager,
		resyncBuf:     resyncBuf,
		subscriptions: map[syntax.DID]*subscription{},
	}
}

// addSubscription adds a new subscription to subscribeSpaces and tracks it by org id
func (s *subscriber) addSubscription(ctx context.Context, org *managedOrg) {
	client := sse.NewClient(org.Host + "/xrpc/network.habitat.sync.subscribeSpaces")
	client.Connection = s.orgManager.GetClient(ctx, org.DID)
	client.LastEventID.Store([]byte(org.SubscribeCursor))
	lastGoodCursor := []byte(org.SubscribeCursor)
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
			err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				if err := tx.Model(&managedOrg{}).
					Where("did = ?", org.DID).
					Update("subscribe_cursor", strconv.FormatUint(spaceEvent.Seq, 10)).
					Error; err != nil {
					return err
				}
				var currentOrg managedOrg
				if err := tx.First(&currentOrg, "did = ?", org.DID).Error; err != nil {
					return err
				}
				return s.resyncBuf.WithTx(tx).handleSpaceEvent(ctx, &currentOrg, spaceEvent)
			})
			if err != nil {
				slog.ErrorContext(subscribeCtx, "failed to save space event", "err", err)
				// The sse client already advanced LastEventID past this event
				// before invoking this handler. Roll it back to the last
				// successfully committed cursor so a reconnect replays the
				// unacked event instead of skipping it.
				client.LastEventID.Store(lastGoodCursor)
				return
			}
			lastGoodCursor = []byte(strconv.FormatUint(spaceEvent.Seq, 10))
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

func (s *subscriber) cancelSubscription(orgDID syntax.DID) {
	s.subscriptionsMu.Lock()
	defer s.subscriptionsMu.Unlock()
	if sub, ok := s.subscriptions[orgDID]; ok {
		sub.cancel()
		delete(s.subscriptions, orgDID)
	}
}

// closeSubscriptions cleans up the subscriptions. The cursor is already
// persisted per-event in handleSpaceEvent's transaction, so we only cancel.
func (s *subscriber) closeSubscriptions() error {
	s.subscriptionsMu.Lock()
	for did, sub := range s.subscriptions {
		sub.cancel()
		delete(s.subscriptions, did)
	}
	s.subscriptionsMu.Unlock()
	return nil
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
