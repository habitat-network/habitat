package spaces_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestGroupSpaceIndexLifecycle(t *testing.T) {
	ctx := t.Context()
	db := db_testutil.NewDB(t, spaces.Models())
	store := spaces_testutil.NewTestStore(t, spaces_testutil.WithDB(db))
	members, err := store.CreateSpace(ctx, orgID, habitat_syntax.AtgrpMembersSpaceType, "self")
	require.NoError(t, err)
	policy := spaces.Policy{Type: spaces.PolicyAtgrp}
	legacy, err := store.CreateSpace(ctx, orgID, "com.example.legacy", "self")
	require.NoError(t, err)
	require.ErrorIs(t, spaces.SyncGroupSpaceIndex(ctx, store, legacy), spaces.ErrUnsupportedPolicy)
	require.NoError(t, spaces.SetGroupSpacePolicy(ctx, store, members, policy))
	require.ErrorIs(t, spaces.SyncGroupSpaceIndex(ctx, store, legacy), spaces.ErrUnsupportedPolicy)

	var (
		listed   habitat_syntax.SpaceURI
		unlisted habitat_syntax.SpaceURI
	)
	require.NoError(t, spaces.Transaction(ctx, db, func(tx *gorm.DB) error {
		s := store.WithTx(tx)
		var err error
		listed, err = s.CreateSpace(ctx, orgID, "com.example.listed", "self")
		require.NoError(t, err)
		require.NoError(t, spaces.SetGroupSpacePolicy(ctx, s, listed, policy))
		require.NoError(t, spaces.SyncGroupSpaceIndex(ctx, s, listed))
		require.NoError(t, spaces.SyncGroupSpaceIndex(ctx, s, listed))
		require.NoError(t, spaces.SyncGroupSpaceIndex(ctx, s, members))
		unlisted, err = s.CreateSpace(ctx, orgID, "com.example.unlisted", "self")
		require.NoError(t, err)
		private := policy
		private.Unlisted = true
		require.NoError(t, spaces.SetGroupSpacePolicy(ctx, s, unlisted, private))
		require.NoError(t, spaces.SyncGroupSpaceIndex(ctx, s, unlisted))
		return nil
	}))

	index := func(want ...habitat_syntax.SpaceURI) []spaces.Record {
		t.Helper()
		collection := habitat_syntax.AtgrpSpaceCollection
		records, err := store.ListRecords(ctx, members, orgID, &collection)
		require.NoError(t, err)
		var refs []string
		for _, record := range records {
			_, err := syntax.ParseTID(record.Rkey.String())
			require.NoError(t, err)
			_, err = time.Parse(time.RFC3339Nano, record.Value["createdAt"].(string))
			require.NoError(t, err)
			refs = append(refs, record.Value["space"].(string))
		}
		var desired []string
		for _, ref := range want {
			desired = append(desired, ref.AtSpace().String())
		}
		require.ElementsMatch(t, desired, refs)
		return records
	}
	entry := index(listed)[0]
	meta, err := store.CreateSpace(ctx, orgID, habitat_syntax.AtgrpMetaSpaceType, "self")
	require.NoError(t, err)
	require.NoError(t, spaces.SetGroupSpacePolicy(ctx, store, meta, policy))
	require.NoError(t, spaces.SyncGroupSpaceIndex(ctx, store, meta))
	require.Equal(t, entry.Rkey, index(listed)[0].Rkey)
	rollback := errors.New("rollback")
	require.ErrorIs(t, spaces.Transaction(ctx, db, func(tx *gorm.DB) error {
		s := store.WithTx(tx)
		require.NoError(t, s.DeleteSpace(ctx, listed))
		require.NoError(t, spaces.SyncGroupSpaceIndex(ctx, s, listed))
		return rollback
	}), rollback)
	require.Equal(t, entry.Rkey, index(listed)[0].Rkey)

	require.NoError(t, spaces.Transaction(ctx, db, func(tx *gorm.DB) error {
		s := store.WithTx(tx)
		private := policy
		private.Unlisted = true
		require.NoError(t, spaces.SetGroupSpacePolicy(ctx, s, listed, private))
		require.NoError(t, spaces.SyncGroupSpaceIndex(ctx, s, listed))
		require.NoError(t, spaces.SetGroupSpacePolicy(ctx, s, unlisted, policy))
		return spaces.SyncGroupSpaceIndex(ctx, s, unlisted)
	}))
	index(unlisted)

	require.NoError(t, spaces.Transaction(ctx, db, func(tx *gorm.DB) error {
		s := store.WithTx(tx)
		require.NoError(t, s.DeleteSpace(ctx, unlisted))
		return spaces.SyncGroupSpaceIndex(ctx, s, unlisted)
	}))
	index()
	_, err = store.GetSpacePolicy(ctx, listed)
	require.NoError(t, err)
}

func TestGroupWriterReconciliation(t *testing.T) {
	ctx := t.Context()
	db := db_testutil.NewDB(t, spaces.Models())
	store := spaces_testutil.NewTestStore(t, spaces_testutil.WithDB(db))
	const (
		alice = syntax.DID("did:plc:alice")
		bob   = syntax.DID("did:plc:bob")
	)
	original := spaces.Policy{
		Type: spaces.PolicyAtgrp, PublicRead: true,
		Grants: []spaces.AtgrpGrant{{Role: "readers", Read: true}, {Role: "writers", Write: true}},
	}
	read, write := original.AtgrpAccess(nil)
	require.True(t, read)
	require.False(t, write)
	original.PublicRead = false
	read, write = original.AtgrpAccess([]string{"writers"})
	require.False(t, read, "write does not imply read")
	require.True(t, write)
	read, write = original.AtgrpAccess([]string{"writers", "readers"})
	require.True(t, read)
	require.True(t, write)

	listed, err := store.CreateSpace(ctx, orgID, "com.example.listed", "self")
	require.NoError(t, err)
	require.NoError(t, spaces.SetGroupSpacePolicy(ctx, store, listed, original))
	unlisted, err := store.CreateSpace(ctx, orgID, "com.example.unlisted", "self")
	require.NoError(t, err)
	hidden := original
	hidden.Unlisted = true
	require.NoError(t, spaces.SetGroupSpacePolicy(ctx, store, unlisted, hidden))
	legacy, err := store.CreateSpace(ctx, orgID, "com.example.legacy", "self")
	require.NoError(t, err)
	for _, uri := range []habitat_syntax.SpaceURI{listed, unlisted, legacy} {
		for _, author := range []syntax.DID{alice, bob} {
			rev := syntax.NewTIDClock(0).Next()
			require.NoError(t, store.RegisterRemoteWrite(ctx, uri, author, rev, []byte{1}))
		}
	}

	require.NoError(t, spaces.Transaction(ctx, db, func(tx *gorm.DB) error {
		return spaces.ReconcileGroupWriter(ctx, store.WithTx(tx), orgID, alice, []string{"readers"})
	}))
	writers := func(uri habitat_syntax.SpaceURI) []syntax.DID {
		t.Helper()
		repos, err := store.ListRepos(ctx, uri)
		require.NoError(t, err)
		dids := make([]syntax.DID, 0, len(repos))
		for _, repo := range repos {
			dids = append(dids, repo.DID)
		}
		return dids
	}
	require.ElementsMatch(t, []syntax.DID{orgID, bob}, writers(listed))
	require.ElementsMatch(t, []syntax.DID{orgID, bob}, writers(unlisted))
	require.ElementsMatch(t, []syntax.DID{alice, bob}, writers(legacy))
	_, changes, err := store.ListWritersSince(ctx, listed, "")
	require.NoError(t, err)
	require.True(t, changes[len(changes)-1].Removed)
	require.Equal(t, alice, changes[len(changes)-1].DID)

	require.NoError(t, spaces.Transaction(ctx, db, func(tx *gorm.DB) error {
		s := store.WithTx(tx)
		require.NoError(t, spaces.SetGroupSpacePolicy(ctx, s, listed,
			spaces.Policy{Type: spaces.PolicyAtgrp, PublicRead: true}))
		return spaces.ReconcileGroupSpaceWriters(ctx, s, listed,
			func(_ context.Context, did syntax.DID) ([]string, error) {
				require.Equal(t, bob, did)
				return []string{"writers"}, nil
			})
	}))
	require.Equal(t, []syntax.DID{orgID}, writers(listed))
	require.ElementsMatch(t, []syntax.DID{orgID, bob}, writers(unlisted))
}
