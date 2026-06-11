package sap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/r3labs/sse/v2"
	"gorm.io/gorm"
)

type subscription struct {
	client *sse.Client
}

type subscriber struct {
	db         *gorm.DB
	orgManager *orgManager

	subscriptionsMu sync.RWMutex
	subscriptions   map[syntax.DID]*subscription

	sseCh chan *sse.Event
}

func newSubscriber(db *gorm.DB, orgManager *orgManager, sseCh chan *sse.Event) *subscriber {
	return &subscriber{
		db:            db,
		orgManager:    orgManager,
		subscriptions: map[syntax.DID]*subscription{},
		sseCh:         sseCh,
	}
}

func (s *subscriber) addSubscription(ctx context.Context, org *managedOrg) {
	client := sse.NewClient(org.Host + "/xrpc/network.habitat.sync.subscribeSpaces")
	client.Connection = s.orgManager.GetClient(ctx, org.DID)
	client.LastEventID.Store([]byte(org.Cursor))
	sub := &subscription{client: client}
	// client.ReconnectStrategy = &backoff.StopBackOff{}
	err := client.SubscribeChanRawWithContext(context.Background(), s.sseCh)
	if err != nil {
		slog.ErrorContext(ctx, "failed to subscribe", "org", org.DID, "err", err)
		s.db.Model(&managedOrg{}).Where("did = ?", org.DID).UpdateColumn("error_msg", err.Error())
		return
	}

	s.subscriptionsMu.Lock()
	s.subscriptions[org.DID] = sub
	s.subscriptionsMu.Unlock()
}

func (s *subscriber) closeSubscriptions() error {
	lastEventIDs := map[syntax.DID]string{}
	s.subscriptionsMu.Lock()
	for did, sub := range s.subscriptions {
		lastEventIDs[did] = string(sub.client.LastEventID.Load().([]byte))
		sub.client.Unsubscribe(s.sseCh)
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
		s.addSubscription(ctx, &o)
	}
	slog.InfoContext(ctx, "loaded subscriptions", "count", len(orgs))
	return nil
}
