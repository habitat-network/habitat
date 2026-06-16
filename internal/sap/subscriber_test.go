package sap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/events"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/internal/sync"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type dummySubscriber struct{ ch chan events.Event }

var _ events.EventStream = (*dummySubscriber)(nil)

// Subscribe implements [events.EventStream].
func (d *dummySubscriber) Subscribe(ctx context.Context, since uint64) <-chan events.Event {
	return d.ch
}

func TestSubscriber(t *testing.T) {
	db, eventsCh, _ := setupSubscriber(t)
	space := habitat_syntax.SpaceURI("ats://did:plc:testorg/test.space/my-space")
	require.NoError(t, db.Create(&managedRepo{
		Space: space,
		DID:   "did:plc:repo1",
		State: RepoStateActive,
	}).Error)
	require.NoError(t, db.Create(&managedRepo{
		Space: space,
		DID:   "did:plc:repo2",
		State: RepoStateActive,
	}).Error)

	clock := syntax.NewTIDClock(0)
	event1 := events.Event{
		Seq:   1,
		Type:  "space",
		Space: space,
		Repo:  "did:plc:repo1",
		Rev:   clock.Next(),
		Ops: []events.EventOps{
			{
				Uri:    "ats://did:plc:testorg/test.space/my-space/did:plc:repo1/test.collection/foo",
				Action: "create",
				Value: map[string]any{
					"foo": "bar",
				},
			},
		},
	}
	eventsCh <- event1

	event2 := events.Event{
		Seq:   8,
		Type:  "space",
		Space: space,
		Repo:  "did:plc:repo2",
		Rev:   clock.Next(),
		Ops: []events.EventOps{
			{
				Uri:    "ats://did:plc:testorg/test.space/my-space/did:plc:repo2/test.collection/foo",
				Action: "update",
				Value: map[string]any{
					"foo": "baz",
				},
			},
		},
	}
	eventsCh <- event2

	event3 := events.Event{
		Seq:   12,
		Type:  "space",
		Space: space,
		Repo:  "did:plc:repo1",
		Rev:   clock.Next(),
		Since: event1.Rev,
		Ops: []events.EventOps{
			{
				Uri:    "ats://did:plc:testorg/test.space/my-space/did:plc:repo1/test.collection/foo",
				Action: "update",
				Value: map[string]any{
					"foo": "baz",
				},
			},
		},
	}
	eventsCh <- event3

	require.Eventually(t, func() bool {
		var records []outbox
		require.NoError(t, db.Find(&records).Error)
		return len(records) == 3
	}, time.Second, 100*time.Millisecond)

	var repos []managedRepo
	require.NoError(t, db.Find(&repos).Error)
	require.Len(t, repos, 2)

	var records []outbox
	require.NoError(t, db.Find(&records).Error)
	require.Equal(t, "{\"foo\":\"baz\"}", string(records[2].Value))
}

func TestSubscriber_MissedEvent(t *testing.T) {
	db, eventsCh, _ := setupSubscriber(t)
	space := habitat_syntax.SpaceURI("ats://did:plc:testorg/test.space/my-space")
	require.NoError(t, db.Create(&managedRepo{
		Space: space,
		DID:   "did:plc:repo1",
		State: RepoStateActive,
	}).Error)

	clock := syntax.NewTIDClock(0)
	prev := clock.Next()
	eventsCh <- events.Event{
		Seq:   1,
		Type:  "space",
		Space: space,
		Repo:  "did:plc:repo1",
		Rev:   clock.Next(),
		Since: prev,
		Ops: []events.EventOps{
			{
				Uri:    "ats://did:plc:testorg/test.space/my-space/did:plc:repo1/test.collection/foo",
				Action: "create",
				Value: map[string]any{
					"foo": "bar",
				},
			},
		},
	}

	require.Eventually(t, func() bool {
		var repos []managedRepo
		require.NoError(t, db.Find(&repos).Error)
		return len(repos) == 1 && repos[0].State == RepoStateDesynced
	}, time.Second, 100*time.Millisecond)

	var records []outbox
	require.NoError(t, db.Find(&records).Error)
	require.Len(t, records, 0)
}

func TestSubscriber_BuffersWhileCrawlRunning(t *testing.T) {
	db, eventsCh, _ := setupSubscriber(t)
	running := crawlStateRunning
	require.NoError(t, db.Model(&managedOrg{}).
		Where("did = ?", "did:plc:testorg").
		Update("crawl_state", running).Error)

	space := habitat_syntax.SpaceURI("ats://did:plc:testorg/test.space/my-space")
	clock := syntax.NewTIDClock(0)
	eventsCh <- events.Event{
		Seq:   1,
		Type:  "space",
		Space: space,
		Repo:  "did:plc:repo1",
		Rev:   clock.Next(),
		Ops: []events.EventOps{
			{
				Uri:    "ats://did:plc:testorg/test.space/my-space/did:plc:repo1/test.collection/foo",
				Action: "create",
				Value:  map[string]any{"foo": "bar"},
			},
		},
	}

	require.Eventually(t, func() bool {
		var buffered []bufferedEvent
		require.NoError(t, db.Find(&buffered).Error)
		return len(buffered) == 1
	}, time.Second, 100*time.Millisecond)

	var records []outbox
	require.NoError(t, db.Find(&records).Error)
	require.Len(t, records, 0)
}

func setupSubscriber(
	t *testing.T,
) (db *gorm.DB, eventsCh chan events.Event, subscriber *subscriber) {
	db, err := gorm.Open(
		sqlite.Open(t.TempDir()+"/test.db"),
		&gorm.Config{},
	)
	require.NoError(t, err)
	require.NoError(t, autoMigrate(db))

	eventsCh = make(chan events.Event)

	srv := httptest.NewServer(
		http.HandlerFunc(sync.NewServer(
			&dummySubscriber{ch: eventsCh},
		).HandleSubscribeSpaces),
	)
	complete := crawlStateComplete
	require.NoError(t, db.Save(&managedOrg{
		DID:         "did:plc:testorg",
		Host:        srv.URL,
		AccessToken: "token",
		ExpiresAt:   time.Now().Add(time.Hour),
		CrawlState:  &complete,
	}).Error)
	t.Cleanup(func() { srv.Close() })

	orgManager := newOrgManager(db, "", nil, pdsclient.NewDummyDirectory("https://pds.example.com"))
	resyncBuf := newResyncBuffer(db)
	subscriber = newSubscriber(db, orgManager, resyncBuf)

	go func() {
		_ = subscriber.loadSubscriptions(t.Context())
	}()
	time.Sleep(100 * time.Millisecond)

	t.Cleanup(func() { require.NoError(t, subscriber.closeSubscriptions()) })

	return db, eventsCh, subscriber
}
