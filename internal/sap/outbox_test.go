package sap

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/events"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func writeOutboxEvent(
	t *testing.T,
	db *gorm.DB,
	resyncBuf *resyncBuffer,
	space habitat_syntax.SpaceURI,
	repoDID syntax.DID,
	rev, since syntax.TID,
) {
	t.Helper()
	recordURI := habitat_syntax.SpaceRecordURI(fmt.Sprintf(
		"ats://did:plc:testorg/network.habitat.space/my-space/%s/network.habitat.note/%s",
		repoDID, rev,
	))
	event := events.Event{
		Seq:   1,
		Space: space,
		Repo:  repoDID,
		Rev:   rev,
		Since: since,
		Ops: []events.EventOps{
			{Uri: recordURI, Action: "create", Value: map[string]any{"text": "hello"}},
		},
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return resyncBuf.WithTx(tx).handleSpaceEvent(t.Context(), &managedOrg{}, event)
	}))
}

func TestOutbox_PollOrdersByIDAndRespectsLimit(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	resyncBuf := newResyncBuffer(db, utils.NewPollNotifier(), utils.NewPollNotifier())
	out := newOutbox(db, utils.NewPollNotifier())

	space := habitat_syntax.SpaceURI("ats://did:plc:testorg/network.habitat.space/my-space")
	repoDID := syntax.DID("did:plc:member1")
	require.NoError(t, ensureRepo(db, t.Context(), space, repoDID))
	require.NoError(t, setActive(db, t.Context(), space, repoDID, ""))

	clock := syntax.NewTIDClock(0)
	prev := syntax.TID("")
	for range 3 {
		rev := clock.Next()
		writeOutboxEvent(t, db, resyncBuf, space, repoDID, rev, prev)
		prev = rev
	}

	msgs, err := out.Poll(t.Context(), 2)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	require.Less(t, msgs[0].ID, msgs[1].ID)
}

func TestOutbox_AckPreventsRedelivery(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	resyncBuf := newResyncBuffer(db, utils.NewPollNotifier(), utils.NewPollNotifier())
	out := newOutbox(db, utils.NewPollNotifier())

	space := habitat_syntax.SpaceURI("ats://did:plc:testorg/network.habitat.space/my-space")
	repoDID := syntax.DID("did:plc:member1")
	require.NoError(t, ensureRepo(db, t.Context(), space, repoDID))
	require.NoError(t, setActive(db, t.Context(), space, repoDID, ""))

	rev := syntax.NewTIDClock(0).Next()
	writeOutboxEvent(t, db, resyncBuf, space, repoDID, rev, "")

	msgs, err := out.Poll(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	require.NoError(t, msgs[0].Ack(t.Context()))

	remaining, err := out.Poll(t.Context(), 10)
	require.NoError(t, err)
	require.Empty(t, remaining)
}

func TestOutbox_PollRedeliversUnackedMessages(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	resyncBuf := newResyncBuffer(db, utils.NewPollNotifier(), utils.NewPollNotifier())
	out := newOutbox(db, utils.NewPollNotifier())

	space := habitat_syntax.SpaceURI("ats://did:plc:testorg/network.habitat.space/my-space")
	repoDID := syntax.DID("did:plc:member1")
	require.NoError(t, ensureRepo(db, t.Context(), space, repoDID))
	require.NoError(t, setActive(db, t.Context(), space, repoDID, ""))

	rev := syntax.NewTIDClock(0).Next()
	writeOutboxEvent(t, db, resyncBuf, space, repoDID, rev, "")

	first, err := out.Poll(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, first, 1)

	second, err := out.Poll(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.Equal(t, first[0].ID, second[0].ID)
}

func TestOutbox_WatchNotifiesOnNewMessage(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	outboxNotif := utils.NewPollNotifier()
	resyncBuf := newResyncBuffer(db, utils.NewPollNotifier(), outboxNotif)
	out := newOutbox(db, outboxNotif)

	space := habitat_syntax.SpaceURI("ats://did:plc:testorg/network.habitat.space/my-space")
	repoDID := syntax.DID("did:plc:member1")
	require.NoError(t, ensureRepo(db, t.Context(), space, repoDID))
	require.NoError(t, setActive(db, t.Context(), space, repoDID, ""))

	rev := syntax.NewTIDClock(0).Next()
	writeOutboxEvent(t, db, resyncBuf, space, repoDID, rev, "")

	select {
	case <-out.Watch():
	default:
		t.Fatal("expected outbox watch channel to be notified after a new message was written")
	}
}

func TestNewOutboxMessageForTesting_AckInvokesProvidedFunc(t *testing.T) {
	var called bool
	msg := NewOutboxMessageForTesting(
		1,
		habitat_syntax.SpaceRecordURI(
			"ats://did:plc:org1/network.habitat.space/skey1/did:plc:user1/network.habitat.note/rkey1",
		),
		json.RawMessage(`{"title":"hello"}`),
		func(ctx context.Context) error {
			called = true
			return nil
		},
	)
	require.Equal(t, uint(1), msg.ID)
	require.NoError(t, msg.Ack(t.Context()))
	require.True(t, called)
}
