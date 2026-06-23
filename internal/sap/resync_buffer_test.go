package sap

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/events"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func ensureRepo(db *gorm.DB, ctx any, space habitat_syntax.SpaceURI, did syntax.DID) error {
	return db.
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&managedRepo{Space: space, DID: did, State: RepoStatePending}).Error
}

func setActive(
	db *gorm.DB,
	ctx any,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
	rev syntax.TID,
) error {
	return db.
		Model(&managedRepo{}).
		Where("space = ? AND did = ?", space, did).
		Updates(map[string]any{
			"state":       RepoStateActive,
			"rev":         rev,
			"error_msg":   "",
			"retry_count": 0,
			"retry_after": 0,
		}).Error
}

func getRepo(
	db *gorm.DB,
	ctx any,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
) (*managedRepo, error) {
	var repo managedRepo
	err := db.Where("space = ? AND did = ?", space, did).First(&repo).Error
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

func TestResyncBuffer_AppendAndDrain(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	resyncBuf := newResyncBuffer(db, make(chan struct{}, 1), make(chan struct{}, 1))

	space := habitat_syntax.SpaceURI("ats://did:plc:testorg/network.habitat.space/my-space")
	repoDID := syntax.DID("did:plc:member1")
	require.NoError(t, ensureRepo(db, t.Context(), space, repoDID))

	clock := syntax.NewTIDClock(0)
	rev := clock.Next()
	recordURI := habitat_syntax.SpaceRecordURI(
		"ats://did:plc:testorg/network.habitat.space/my-space/did:plc:member1/network.habitat.note/k1",
	)
	event := events.Event{
		Seq:   1,
		Space: space,
		Repo:  repoDID,
		Rev:   rev,
		Ops: []events.EventOps{
			{
				Uri:    recordURI,
				Action: "create",
				Value:  map[string]any{"text": "hello"},
			},
		},
	}

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return resyncBuf.WithTx(tx).appendEvent(event)
	}))

	repo, err := getRepo(db, t.Context(), space, repoDID)
	require.NoError(t, err)
	require.NoError(t, resyncBuf.drainRepo(t.Context(), repo))

	var records []outbox
	require.NoError(t, db.Find(&records).Error)
	require.Len(t, records, 1)

	// drainRepo should have transitioned the repo to Active with the event's rev.
	var updated managedRepo
	require.NoError(t, db.Where("space = ? AND did = ?", space, repoDID).First(&updated).Error)
	require.Equal(t, RepoStateActive, updated.State)
	require.Equal(t, rev, updated.Rev)

	var buffered []bufferedEvent
	require.NoError(t, db.Find(&buffered).Error)
	require.Len(t, buffered, 0)
}

func TestResyncBuffer_DrainSkipsAlreadyCovered(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	resyncBuf := newResyncBuffer(db, make(chan struct{}, 1), make(chan struct{}, 1))

	space := habitat_syntax.SpaceURI("ats://did:plc:testorg/network.habitat.space/my-space")
	repoDID := syntax.DID("did:plc:member1")

	clock := syntax.NewTIDClock(0)
	rev1 := clock.Next()
	rev2 := clock.Next()

	require.NoError(t, ensureRepo(db, t.Context(), space, repoDID))
	require.NoError(t, setActive(db, t.Context(), space, repoDID, rev2))

	recordURI := habitat_syntax.SpaceRecordURI(
		"ats://did:plc:testorg/network.habitat.space/my-space/did:plc:member1/network.habitat.note/k1",
	)

	bufferedEventData := events.Event{
		Seq:   1,
		Space: space,
		Repo:  repoDID,
		Rev:   rev1,
		Since: rev1,
		Ops: []events.EventOps{
			{
				Uri:    recordURI,
				Action: "create",
				Value:  map[string]any{"text": "hello"},
			},
		},
	}
	require.NoError(t, resyncBuf.appendEvent(bufferedEventData))
	repo, err := getRepo(db, t.Context(), space, repoDID)
	require.NoError(t, err)
	require.NoError(t, resyncBuf.drainRepo(t.Context(), repo))

	repo, err = getRepo(db, t.Context(), space, repoDID)
	require.NoError(t, err)
	require.Equal(
		t,
		RepoStateActive,
		repo.State,
		"repo should stay active when buffered event is already covered",
	)

	var remaining []bufferedEvent
	require.NoError(t, db.Find(&remaining).Error)
	require.Len(t, remaining, 0, "stale buffered events should be deleted")

	var records []outbox
	require.NoError(t, db.Find(&records).Error)
	require.Len(t, records, 0, "no outbox records for already-covered events")
}

func TestResyncBuffer_DrainDesyncsOnFutureSince(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	resyncBuf := newResyncBuffer(db, make(chan struct{}, 1), make(chan struct{}, 1))

	space := habitat_syntax.SpaceURI("ats://did:plc:testorg/network.habitat.space/my-space")
	repoDID := syntax.DID("did:plc:member1")

	clock := syntax.NewTIDClock(0)
	rev1 := clock.Next()
	rev2 := clock.Next()

	require.NoError(t, ensureRepo(db, t.Context(), space, repoDID))
	require.NoError(t, setActive(db, t.Context(), space, repoDID, rev1))

	recordURI := habitat_syntax.SpaceRecordURI(
		"ats://did:plc:testorg/network.habitat.space/my-space/did:plc:member1/network.habitat.note/k1",
	)

	bufferedEventData := events.Event{
		Seq:   1,
		Space: space,
		Repo:  repoDID,
		Rev:   rev2,
		Since: rev2,
		Ops: []events.EventOps{
			{
				Uri:    recordURI,
				Action: "create",
				Value:  map[string]any{"text": "hello"},
			},
		},
	}
	require.NoError(t, resyncBuf.appendEvent(bufferedEventData))
	repo, err := getRepo(db, t.Context(), space, repoDID)
	require.NoError(t, err)
	require.NoError(t, resyncBuf.drainRepo(t.Context(), repo))

	repo, err = getRepo(db, t.Context(), space, repoDID)
	require.NoError(t, err)
	require.Equal(
		t,
		RepoStateDesynced,
		repo.State,
		"repo should be desynced when buffered event has Since > current Rev",
	)

	var remaining []bufferedEvent
	require.NoError(t, db.Find(&remaining).Error)
	require.Len(t, remaining, 0, "stale buffered events should be deleted")
}
