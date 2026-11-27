package foreignrepo

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/spacecommit"
	"github.com/habitat-network/habitat/internal/spaces"
)

func takeOne(t *testing.T, s *Store, now time.Time) Delivery {
	t.Helper()
	items, err := s.ClaimDue(t.Context(), now, 1)
	require.NoError(t, err)
	require.Len(t, items, 1)
	return items[0]
}

func noDue(t *testing.T, s *Store, now time.Time) {
	t.Helper()
	items, err := s.ClaimDue(t.Context(), now, maxClaim)
	require.NoError(t, err)
	require.Empty(t, items)
}

func checkDelivery(t *testing.T, item Delivery, commit spacecommit.Commit) {
	t.Helper()
	require.Equal(t, commit.Space, item.Space)
	require.Equal(t, commit.Repo, item.Author)
	require.Equal(t, commit.Rev, item.Rev.String())
	require.Equal(t, commit.Data, item.Digest[:])
}

func TestOutboxCoalescesAndRejectsStaleAcknowledgement(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	uri := inviteURI(recipient, f.first, key)
	initial, err := f.authored.Apply(ctx, uri, spaces.WriteCreate, invite("initial"), "")
	require.NoError(t, err)
	now := time.Now().Add(time.Second)
	old := takeOne(t, f.authored, now)
	checkDelivery(t, old, initial)
	noDue(t, f.authored, now)

	live, err := f.authored.Get(ctx, uri)
	require.NoError(t, err)
	_, err = f.authored.Apply(
		ctx,
		uri,
		spaces.WriteUpdate,
		invite("replacement"),
		live.CID.String(),
	)
	require.NoError(t, err)
	live, err = f.authored.Get(ctx, uri)
	require.NoError(t, err)
	head, err := f.authored.Apply(ctx, uri, spaces.WriteDelete, nil, live.CID.String())
	require.NoError(t, err)
	require.Greater(t, head.Rev, initial.Rev)
	changed, err := f.authored.Complete(ctx, old, now, Delivered)
	require.NoError(t, err)
	require.False(t, changed)
	changed, err = f.authored.Complete(ctx, old, now, Retry)
	require.NoError(t, err)
	require.False(t, changed)
	latest := takeOne(t, f.authored, now)
	checkDelivery(t, latest, head)
	require.Equal(t, 1, latest.attempt, "a new revision is immediately due")
	changed, err = f.authored.Complete(ctx, latest, now, Delivered)
	require.NoError(t, err)
	require.True(t, changed)
	noDue(t, f.authored, now.Add(leaseTime))
}

func TestOutboxNoOpAndOuterRollback(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	uri := inviteURI(recipient, f.first, key)
	first, err := f.authored.Apply(ctx, uri, spaces.WriteCreate, invite("before"), "")
	require.NoError(t, err)
	record, err := f.authored.Get(ctx, uri)
	require.NoError(t, err)
	rollback := errors.New("rollback outer transaction")
	require.ErrorIs(t, f.db.Transaction(func(tx *gorm.DB) error {
		_, err := f.authored.WithTx(tx).
			Apply(ctx, uri, spaces.WriteDelete, nil, record.CID.String())
		require.NoError(t, err)
		return rollback
	}), rollback)
	now := time.Now().Add(time.Second)
	item := takeOne(t, f.authored, now)
	checkDelivery(t, item, first)
	ok, err := f.authored.Complete(ctx, item, now, Delivered)
	require.NoError(t, err)
	require.True(t, ok)
	_, err = f.authored.Apply(ctx, uri, spaces.WriteUpdate, invite("before"), record.CID.String())
	require.NoError(t, err)
	noDue(t, f.authored, now)
}

func TestOutboxRetryAndCrashRecoveryAfterRestart(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	uri := inviteURI(recipient, f.first, key)
	head, err := f.authored.Apply(ctx, uri, spaces.WriteCreate, invite("queued"), "")
	require.NoError(t, err)
	now := time.Now().Add(time.Second)
	initial := takeOne(t, f.authored, now)
	ok, err := f.authored.Complete(ctx, initial, now, Retry)
	require.NoError(t, err)
	require.True(t, ok)
	pool, err := f.db.DB()
	require.NoError(t, err)
	require.NoError(t, pool.Close())
	reopened := openFixture(t, f.path)
	due := now.Add(firstBackoff)
	noDue(t, reopened.authored, due.Add(-time.Millisecond))
	second := takeOne(t, reopened.authored, due)
	checkDelivery(t, second, head)
	require.Equal(t, 2, second.attempt)
	noDue(t, reopened.authored, due.Add(leaseTime-time.Millisecond))
	ok, err = reopened.authored.Complete(ctx, second, due.Add(leaseTime), Delivered)
	require.NoError(t, err)
	require.False(t, ok, "an expired lease cannot complete even before another claim")
	third := takeOne(t, reopened.authored, due.Add(leaseTime))
	require.Equal(t, 3, third.attempt)
	ok, err = reopened.authored.Complete(ctx, second, due.Add(leaseTime), Delivered)
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = reopened.authored.Complete(ctx, third, due.Add(leaseTime), PermanentFailure)
	require.NoError(t, err)
	require.True(t, ok)
	noDue(t, reopened.authored, due.Add(2*leaseTime))
}

func TestOutboxConcurrentClaims(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	pool := openFixture(t, f.path)
	a, err := f.authored.Apply(
		ctx,
		inviteURI(recipient, f.first, key),
		spaces.WriteCreate,
		invite("one"),
		"",
	)
	require.NoError(t, err)
	b, err := f.authored.Apply(
		ctx,
		inviteURI(recipient, f.second, key),
		spaces.WriteCreate,
		invite("two"),
		"",
	)
	require.NoError(t, err)
	c, err := f.authored.Apply(
		ctx,
		inviteURI("did:plc:secondforeign", f.first, key),
		spaces.WriteCreate,
		invite("three"),
		"",
	)
	require.NoError(t, err)

	now := time.Now()
	start := make(chan struct{})
	var runners errgroup.Group
	var claims [3][]Delivery
	for i, store := range []*Store{f.authored, pool.authored, pool.authored} {
		runners.Go(func() error {
			<-start
			items, err := store.ClaimDue(ctx, now, maxClaim)
			claims[i] = items
			return err
		})
	}
	close(start)
	require.NoError(t, runners.Wait())
	all := append(append(claims[0], claims[1]...), claims[2]...)
	require.Len(t, all, 3)
	for _, commit := range []spacecommit.Commit{a, b, c} {
		var matches []Delivery
		for _, item := range all {
			if item.Space == commit.Space && item.Author == commit.Repo {
				matches = append(matches, item)
			}
		}
		require.Len(t, matches, 1)
		checkDelivery(t, matches[0], commit)
	}
	noDue(t, f.authored, now)
}

func TestOutboxPrunesExpiredAndExhaustedLeases(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	uri := inviteURI(recipient, f.first, key)
	_, err := f.authored.Apply(ctx, uri, spaces.WriteCreate, invite("queued"), "")
	require.NoError(t, err)
	now := time.Now().Add(time.Second)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		item := takeOne(t, f.authored, now)
		require.Equal(t, attempt, item.attempt)
		now = now.Add(leaseTime) // simulate a worker crash on every attempt
	}
	noDue(t, f.authored, now)
	_, err = f.authored.Apply(
		ctx,
		inviteURI(recipient, f.second, key),
		spaces.WriteCreate,
		invite("expires"),
		"",
	)
	require.NoError(t, err)
	noDue(t, f.authored, now.Add(deliveryAge))
	var count int64
	require.NoError(t, f.db.Model(&inviteDelivery{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestOutboxSaturationRollsBackRecordAndHead(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	uri := inviteURI(recipient, f.first, key)
	before, err := f.authored.Apply(ctx, uri, spaces.WriteCreate, invite("before"), "")
	require.NoError(t, err)
	prior, err := f.authored.Get(ctx, uri)
	require.NoError(t, err)
	now := time.Now().Add(time.Second)
	old := takeOne(t, f.authored, now)
	ok, err := f.authored.Complete(ctx, old, now, Delivered)
	require.NoError(t, err)
	require.True(t, ok)
	other := inviteURI(recipient, f.second, key)
	_, err = f.authored.Apply(ctx, other, spaces.WriteCreate, invite("active"), "")
	require.NoError(t, err)
	active, err := f.authored.Get(ctx, other)
	require.NoError(t, err)

	seeds := make([]inviteDelivery, 0, maxPending-1)
	for i := 0; i < cap(seeds); i++ {
		k := scope{
			inbox:  inbox(syntax.DID(fmt.Sprintf("did:plc:capacity%06d", i))),
			author: f.first,
		}
		seeds = append(seeds, inviteDelivery{
			Key: k.key(), Rev: old.Rev, Digest: before.Data, ReadyAt: now.UnixMilli(),
			ExpiresAt: now.Add(deliveryAge).UnixMilli(),
		})
	}
	require.NoError(t, f.db.CreateInBatches(&seeds, 90).Error)
	_, err = f.authored.Apply(
		ctx,
		uri,
		spaces.WriteUpdate,
		invite("must roll back"),
		prior.CID.String(),
	)
	require.ErrorIs(t, err, ErrOutboxFull)
	head, records, err := f.authored.Snapshot(ctx, inbox(recipient), f.first)
	require.NoError(t, err)
	require.Equal(t, before, head)
	require.Equal(t, []Record{prior}, records)
	unseen := syntax.DID("did:plc:newrecipient")
	_, err = f.authored.Apply(
		ctx,
		inviteURI(unseen, f.first, key),
		spaces.WriteCreate,
		invite("new"),
		"",
	)
	require.ErrorIs(t, err, ErrOutboxFull)
	_, err = f.authored.Get(ctx, inviteURI(unseen, f.first, key))
	require.ErrorIs(t, err, spaces.ErrRecordNotFound)
	_, _, err = f.authored.Snapshot(ctx, inbox(unseen), f.first)
	require.ErrorIs(t, err, spaces.ErrRepoNotFound)
	_, err = f.authored.Apply(
		ctx,
		other,
		spaces.WriteUpdate,
		invite("replace at capacity"),
		active.CID.String(),
	)
	require.NoError(t, err)
	items, err := f.authored.ClaimDue(ctx, now, 1000000)
	require.NoError(t, err)
	require.Len(t, items, maxClaim)
}
