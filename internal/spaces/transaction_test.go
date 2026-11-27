package spaces_test

import (
	"errors"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	notify_testutil "github.com/habitat-network/habitat/internal/notify/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestTransactionNotifications(t *testing.T) {
	ctx := t.Context()
	db := db_testutil.NewDB(t, spaces.Models())
	notifications := &notify_testutil.TestNotifier{}
	s := spaces_testutil.NewTestStore(
		t,
		spaces_testutil.WithDB(db),
		spaces_testutil.WithNotifier(notifications),
	)
	uri, err := s.CreateSpace(ctx, orgID, groupType, "notifications")
	require.NoError(t, err)
	toDelete, err := s.CreateSpace(ctx, orgID, groupType, "to-delete")
	require.NoError(t, err)

	const collection = syntax.NSID("com.example.notification")
	insert := func(tx *gorm.DB, rkey syntax.RecordKey) {
		// Replacing the DB context must not lose its notification queue.
		txStore := s.WithTx(tx.WithContext(ctx))
		value := spaces_testutil.MustMarshalRecord(t, map[string]any{"$type": collection})
		_, _, err := txStore.PutRecord(ctx, uri, orgID, collection, rkey, value)
		require.NoError(t, err)
	}
	rollback := errors.New("test rollback")
	policy := spaces.Policy{Type: spaces.PolicyAtgrp}
	require.NoError(t, spaces.Transaction(ctx, db, func(tx *gorm.DB) error {
		insert(tx, "before")
		require.ErrorIs(t, spaces.Transaction(ctx, tx, func(child *gorm.DB) error {
			insert(child, "discarded")
			require.NoError(t, s.WithTx(child).DeleteSpace(ctx, toDelete))
			require.Empty(t, notifications.Writes)
			require.Empty(t, notifications.Deleted)
			return rollback
		}), rollback)
		require.NoError(t, spaces.SetGroupSpacePolicy(ctx, s.WithTx(tx), uri, policy))
		require.NoError(t, spaces.Transaction(ctx, tx, func(child *gorm.DB) error {
			insert(child, "kept")
			return nil
		}))
		insert(tx, "after")
		require.NoError(t, s.WithTx(tx).DeleteSpace(ctx, toDelete))
		require.Empty(t, notifications.Writes)
		require.Empty(t, notifications.Deleted)
		return nil
	}))
	require.Len(t, notifications.Writes, 4)
	require.Equal(t, []habitat_syntax.SpaceURI{toDelete}, notifications.Deleted)
	_, err = s.GetRecord(ctx, uri, orgID, collection, "discarded")
	require.ErrorIs(t, err, spaces.ErrRecordNotFound)
	for i, rkey := range []syntax.RecordKey{"before", "self", "kept", "after"} {
		col := collection
		if rkey == "self" {
			col = habitat_syntax.SpacePolicyCollection
		}
		record, err := s.GetRecord(ctx, uri, orgID, col, rkey)
		require.NoError(t, err)
		require.Equal(t, record.Rev, notifications.Writes[i].Rev.String())
	}

	require.ErrorIs(t, spaces.Transaction(ctx, db, func(tx *gorm.DB) error {
		require.NoError(t, spaces.Transaction(ctx, tx, func(child *gorm.DB) error {
			insert(child, "outer-discarded")
			require.NoError(t, s.WithTx(child).RemoveWriter(ctx, uri, orgID))
			return nil
		}))
		return rollback
	}), rollback)
	require.Len(t, notifications.Writes, 4)
	require.Empty(t, notifications.Removed)
	_, err = s.GetRecord(ctx, uri, orgID, collection, "outer-discarded")
	require.ErrorIs(t, err, spaces.ErrRecordNotFound)

	require.Error(t, spaces.Transaction(ctx, db, func(tx *gorm.DB) error {
		insert(tx, "commit-failed")
		// Abort the SQL transaction first so GORM's ensuing commit fails.
		require.NoError(t, tx.Rollback().Error)
		return nil
	}))
	require.Len(t, notifications.Writes, 4)
	_, err = s.GetRecord(ctx, uri, orgID, collection, "commit-failed")
	require.ErrorIs(t, err, spaces.ErrRecordNotFound)

	require.NoError(t, spaces.Transaction(ctx, db, func(tx *gorm.DB) error {
		require.NoError(t, s.WithTx(tx).RemoveWriter(ctx, uri, orgID))
		require.Empty(t, notifications.Removed)
		return nil
	}))
	require.Len(t, notifications.Removed, 1)

	const remote = syntax.DID("did:plc:remote")
	require.NoError(t, spaces.Transaction(ctx, db, func(tx *gorm.DB) error {
		hash := []byte{1, 2, 3}
		rev := syntax.NewTIDClock(0).Next()
		require.NoError(t, s.WithTx(tx).RegisterRemoteWrite(ctx, uri, remote, rev, hash))
		hash[0] = 9
		require.Len(t, notifications.Writes, 4)
		return nil
	}))
	require.Len(t, notifications.Writes, 5)
	require.Equal(t, []byte{1, 2, 3}, notifications.Writes[4].Hash)
}

func TestTransactionRejectsUnmanagedParent(t *testing.T) {
	ctx := t.Context()
	db := db_testutil.NewDB(t, spaces.Models())
	s := spaces_testutil.NewTestStore(t, spaces_testutil.WithDB(db))
	uri, err := s.CreateSpace(ctx, orgID, groupType, "unmanaged")
	require.NoError(t, err)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		err := spaces.Transaction(ctx, tx, func(*gorm.DB) error {
			t.Fatal("unmanaged transaction must not run callback")
			return nil
		})
		require.ErrorIs(t, err, spaces.ErrUnmanagedTransaction)
		policy := spaces.Policy{Type: spaces.PolicyAtgrp}
		require.ErrorIs(t, spaces.SetGroupSpacePolicy(ctx, s.WithTx(tx), uri, policy),
			spaces.ErrUnmanagedTransaction)
		return nil
	}))
	policy, err := s.GetSpacePolicy(ctx, uri)
	require.NoError(t, err)
	require.Equal(t, spaces.DefaultPolicy(), policy)

	require.NoError(
		t,
		spaces.Transaction(ctx, db.Session(&gorm.Session{DisableNestedTransaction: true}),
			func(tx *gorm.DB) error {
				err := spaces.Transaction(ctx, tx, func(*gorm.DB) error {
					t.Fatal("nested transaction without savepoints must not run callback")
					return nil
				})
				require.ErrorIs(t, err, spaces.ErrSavepointsDisabled)
				return nil
			}),
	)
}
