package spaces_test

import (
	"errors"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	dbtest "github.com/habitat-network/habitat/internal/db/testutil"
	notifytest "github.com/habitat-network/habitat/internal/notify/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	spacestest "github.com/habitat-network/habitat/internal/spaces/testutil"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAtomicCreatesCommitAndConflict(t *testing.T) {
	notifier := &notifytest.TestNotifier{}
	store := spacestest.NewTestStore(t, spacestest.WithNotifier(notifier))
	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "atomic")
	require.NoError(t, err)
	record := func(key string) spaces.CreateWrite {
		return spaces.CreateWrite{
			Collection: "email.atmos.messageState",
			Rkey:       syntax.RecordKey(key),
			Value: spacestest.MustMarshalRecord(
				t,
				map[string]any{"$type": "email.atmos.messageState", "state": key},
			),
		}
	}
	results, err := store.ApplyCreates(
		t.Context(),
		uri,
		owner,
		[]spaces.CreateWrite{record("message"), record("operation")},
	)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Len(t, notifier.Writes, 1, "publish only the final committed head")
	rev, hash, found, err := store.RepoHead(t.Context(), uri, owner)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, rev, notifier.Writes[0].Rev.String())
	require.Equal(t, hash, notifier.Writes[0].Hash)

	results, err = store.ApplyCreates(
		t.Context(),
		uri,
		owner,
		[]spaces.CreateWrite{record("must-rollback"), record("operation")},
	)
	require.ErrorIs(t, err, spaces.ErrRecordAlreadyExists)
	require.Nil(t, results)
	_, err = store.GetRecord(t.Context(), uri, owner, "email.atmos.messageState", "must-rollback")
	require.ErrorIs(t, err, spaces.ErrRecordNotFound)
	afterRev, afterHash, _, err := store.RepoHead(t.Context(), uri, owner)
	require.NoError(t, err)
	require.Equal(t, rev, afterRev)
	require.Equal(t, hash, afterHash)
	require.Len(t, notifier.Writes, 1, "rolled-back writes must not notify")
}

func TestAtomicCreatesRollsBackStorageFailure(t *testing.T) {
	db := dbtest.NewDB(t)
	notifier := &notifytest.TestNotifier{}
	store := spacestest.NewTestStore(t, spacestest.WithDB(db), spacestest.WithNotifier(notifier))
	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "fault")
	require.NoError(t, err)
	fault := errors.New("injected second record failure")
	calls := 0
	require.NoError(
		t,
		db.Callback().
			Create().
			Before("gorm:create").
			Register("atomic-test-failure", func(tx *gorm.DB) {
				if tx.Statement.Table == "space_records" {
					calls++
					if calls == 2 {
						_ = tx.AddError(fault)
					}
				}
			}),
	)
	t.Cleanup(func() { require.NoError(t, db.Callback().Create().Remove("atomic-test-failure")) })
	value := spacestest.MustMarshalRecord(
		t,
		map[string]any{"$type": "email.atmos.messageState", "value": "synthetic"},
	)
	results, err := store.ApplyCreates(
		t.Context(),
		uri,
		owner,
		[]spaces.CreateWrite{
			{Collection: "email.atmos.messageState", Rkey: "first", Value: value},
			{Collection: "email.atmos.messageState", Rkey: "second", Value: value},
		},
	)
	require.ErrorIs(t, err, fault)
	require.Nil(t, results)
	_, err = store.GetRecord(t.Context(), uri, owner, "email.atmos.messageState", "first")
	require.ErrorIs(t, err, spaces.ErrRecordNotFound)
	_, _, found, err := store.RepoHead(t.Context(), uri, owner)
	require.NoError(t, err)
	require.False(t, found)
	require.Empty(t, notifier.Writes)
}

func TestAtomicCreatesRejectsMalformedBatch(t *testing.T) {
	store := spacestest.NewTestStore(t)
	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "bounds")
	require.NoError(t, err)
	value := spacestest.MustMarshalRecord(t, map[string]any{"value": "synthetic"})
	good := spaces.CreateWrite{Collection: "email.atmos.messageState", Rkey: "same", Value: value}
	for _, writes := range [][]spaces.CreateWrite{nil, make([]spaces.CreateWrite, 101), {good, good}, {{Collection: good.Collection, Value: value}}} {
		results, err := store.ApplyCreates(t.Context(), uri, owner, writes)
		require.ErrorIs(t, err, spaces.ErrInvalidBatch)
		require.Nil(t, results)
	}
}

func TestAtomicCreatesRejectsCallerTransactionAndDeletedKeys(t *testing.T) {
	db := dbtest.NewDB(t)
	store := spacestest.NewTestStore(t, spacestest.WithDB(db))
	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "deleted")
	require.NoError(t, err)
	write := spaces.CreateWrite{
		Collection: "email.atmos.messageState",
		Rkey:       "claim",
		Value:      spacestest.MustMarshalRecord(t, map[string]any{"value": "synthetic"}),
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := store.WithTx(tx).
			ApplyCreates(t.Context(), uri, owner, []spaces.CreateWrite{write})
		require.ErrorIs(t, err, spaces.ErrInvalidBatch)
		return nil
	}))
	_, err = store.ApplyCreates(t.Context(), uri, owner, []spaces.CreateWrite{write})
	require.NoError(t, err)
	require.NoError(
		t,
		store.DeleteRecord(t.Context(), uri, owner, write.Collection, write.Rkey.String()),
	)
	_, err = store.ApplyCreates(t.Context(), uri, owner, []spaces.CreateWrite{write})
	require.ErrorIs(t, err, spaces.ErrRecordAlreadyExists)
}
