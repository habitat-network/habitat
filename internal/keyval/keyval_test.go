package keyval

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/db/testutil"
)

// payload is the value shape the suite stores.
type payload struct {
	Name string
	N    int
}

// runStoreSuite runs the behaviour every Store must have, whatever the dialect
// behind it, against the store open returns.
//
// open hands back both the store and the database it is built on, because a few
// cases need to look at the rows directly or to build a second store over the
// same database. Each test opens its own, so they are independent.
func runStoreSuite(t *testing.T, open func(t *testing.T) (Store, *gorm.DB)) {
	t.Run("PutGetRoundTrip", func(t *testing.T) {
		s, _ := open(t)
		want := payload{Name: "alpha", N: 1}

		require.NoError(t, s.Put(t.Context(), "key", want, time.Minute))

		var got payload
		require.NoError(t, s.Get(t.Context(), "key", &got))
		require.Equal(t, want, got)
	})

	t.Run("PutUpserts", func(t *testing.T) {
		s, db := open(t)

		require.NoError(t, s.Put(t.Context(), "key", payload{Name: "first", N: 1}, 10*time.Minute))
		require.NoError(t, s.Put(t.Context(), "key", payload{Name: "second", N: 2}, time.Minute))

		var got payload
		require.NoError(t, s.Get(t.Context(), "key", &got))
		require.Equal(t, payload{Name: "second", N: 2}, got)

		// An upsert replaces the row rather than adding a second one, so the
		// second Put's expiry is the only one the key has.
		var rows int64
		require.NoError(t, db.Model(&entry{}).Count(&rows).Error)
		require.EqualValues(t, 1, rows)
	})

	t.Run("GetMissingIsNotFound", func(t *testing.T) {
		s, _ := open(t)

		var got payload
		require.ErrorIs(t, s.Get(t.Context(), "never-written", &got), ErrNotFound)
	})

	t.Run("GetExpiredIsNotFound", func(t *testing.T) {
		s, _ := open(t)
		require.NoError(t, s.Put(t.Context(), "key", payload{Name: "gone", N: 0}, -time.Second))

		var got payload
		require.ErrorIs(t, s.Get(t.Context(), "key", &got), ErrNotFound)
	})

	t.Run("DeleteRemoves", func(t *testing.T) {
		s, _ := open(t)
		require.NoError(t, s.Put(t.Context(), "key", payload{Name: "alpha", N: 1}, time.Minute))

		require.NoError(t, s.Delete(t.Context(), "key"))

		var got payload
		require.ErrorIs(t, s.Get(t.Context(), "key", &got), ErrNotFound)
	})

	t.Run("DeleteMissingIsNotFound", func(t *testing.T) {
		s, _ := open(t)

		require.ErrorIs(t, s.Delete(t.Context(), "never-written"), ErrNotFound)
	})

	t.Run("DeleteExpiredIsNotFound", func(t *testing.T) {
		s, _ := open(t)
		require.NoError(t, s.Put(t.Context(), "key", payload{Name: "gone", N: 0}, -time.Second))

		// Expiry alone does not make a key deletable: the row is as invisible as
		// one that was never written.
		require.ErrorIs(t, s.Delete(t.Context(), "key"), ErrNotFound)
	})

	t.Run("PutIfAbsentKeepsTheFirstWriter", func(t *testing.T) {
		s, _ := open(t)
		require.NoError(
			t,
			s.PutIfAbsent(t.Context(), "key", payload{Name: "first", N: 1}, time.Minute),
		)
		require.ErrorIs(
			t,
			s.PutIfAbsent(t.Context(), "key", payload{Name: "second", N: 2}, time.Minute),
			ErrAlreadyExists,
		)

		var got payload
		require.NoError(t, s.Get(t.Context(), "key", &got))
		require.Equal(t, payload{Name: "first", N: 1}, got)
	})

	t.Run("PutIfAbsentReplacesAnExpiredEntry", func(t *testing.T) {
		s, _ := open(t)
		require.NoError(t, s.Put(t.Context(), "key", payload{Name: "gone", N: 0}, -time.Second))

		require.NoError(
			t,
			s.PutIfAbsent(t.Context(), "key", payload{Name: "fresh", N: 1}, time.Minute),
		)

		var got payload
		require.NoError(t, s.Get(t.Context(), "key", &got))
		require.Equal(t, payload{Name: "fresh", N: 1}, got)
	})

	t.Run("ConcurrentPutIfAbsentYieldsOneWinner", func(t *testing.T) {
		s, _ := open(t)

		const writers = 8
		errs := make([]error, writers)
		var wg sync.WaitGroup
		for i := range writers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs[i] = s.PutIfAbsent(t.Context(), "key", payload{N: i}, time.Minute)
			}()
		}
		wg.Wait()

		wins := 0
		for _, err := range errs {
			if err == nil {
				wins++
			} else {
				require.ErrorIs(t, err, ErrAlreadyExists)
			}
		}
		require.Equal(t, 1, wins)
	})

	t.Run("ConsumeReturnsAndRemoves", func(t *testing.T) {
		s, _ := open(t)
		want := payload{Name: "single-use", N: 1}
		require.NoError(t, s.Put(t.Context(), "key", want, time.Minute))

		var got payload
		require.NoError(t, s.Consume(t.Context(), "key", &got))
		require.Equal(t, want, got)

		var again payload
		require.ErrorIs(t, s.Consume(t.Context(), "key", &again), ErrNotFound)
	})

	t.Run("ConsumeExpiredIsNotFound", func(t *testing.T) {
		s, _ := open(t)
		require.NoError(t, s.Put(t.Context(), "key", payload{Name: "gone", N: 0}, -time.Second))

		var got payload
		require.ErrorIs(t, s.Consume(t.Context(), "key", &got), ErrNotFound)
	})

	t.Run("ConcurrentConsumeYieldsOneWinner", func(t *testing.T) {
		s, _ := open(t)
		require.NoError(
			t,
			s.Put(t.Context(), "key", payload{Name: "single-use", N: 1}, time.Minute),
		)

		const consumers = 8
		errs := make([]error, consumers)
		var wg sync.WaitGroup
		for i := range consumers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var got payload
				errs[i] = s.Consume(t.Context(), "key", &got)
			}()
		}
		wg.Wait()

		var winners int
		for _, err := range errs {
			if err == nil {
				winners++
				continue
			}
			require.ErrorIs(t, err, ErrNotFound)
		}
		require.Equal(t, 1, winners)
	})

	t.Run("NamespaceIsolatesReads", func(t *testing.T) {
		s, _ := open(t)
		require.NoError(t, s.Put(t.Context(), "key", payload{Name: "alpha", N: 1}, time.Minute))

		var got payload
		require.ErrorIs(t, s.Namespace("b").Get(t.Context(), "key", &got), ErrNotFound)
	})

	t.Run("NamespaceIsolatesOverwrite", func(t *testing.T) {
		s, db := open(t)
		require.NoError(t, s.Put(t.Context(), "key", payload{Name: "a", N: 1}, time.Minute))

		b := s.Namespace("b")
		require.NoError(t, b.Put(t.Context(), "key", payload{Name: "b", N: 2}, time.Minute))

		var fromA, fromB payload
		require.NoError(t, s.Get(t.Context(), "key", &fromA))
		require.Equal(t, payload{Name: "a", N: 1}, fromA)
		require.NoError(t, b.Get(t.Context(), "key", &fromB))
		require.Equal(t, payload{Name: "b", N: 2}, fromB)

		// Same key, two namespaces: two rows, not one overwritten.
		var rows int64
		require.NoError(t, db.Model(&entry{}).Count(&rows).Error)
		require.EqualValues(t, 2, rows)
	})

	t.Run("NamespaceIsolatesDelete", func(t *testing.T) {
		s, _ := open(t)
		require.NoError(t, s.Put(t.Context(), "key", payload{Name: "alpha", N: 1}, time.Minute))

		b := s.Namespace("b")
		require.ErrorIs(t, b.Delete(t.Context(), "key"), ErrNotFound)

		var got payload
		require.NoError(t, s.Get(t.Context(), "key", &got))
		require.Equal(t, payload{Name: "alpha", N: 1}, got)
	})

	t.Run("NilDstIsAnErrorNotAPanic", func(t *testing.T) {
		s, _ := open(t)
		require.NoError(t, s.Put(t.Context(), "key", payload{Name: "alpha", N: 1}, time.Minute))

		// Both reject a nil dst before touching the database, so the row is
		// neither read nor consumed by the failed calls.
		require.Error(t, s.Get(t.Context(), "key", nil))
		require.Error(t, s.Consume(t.Context(), "key", nil))

		var got payload
		require.NoError(t, s.Get(t.Context(), "key", &got))
		require.Equal(t, payload{Name: "alpha", N: 1}, got)
	})

	t.Run("PutUnmarshalableValueWritesNothing", func(t *testing.T) {
		s, db := open(t)

		require.Error(t, s.Put(t.Context(), "key", make(chan int), time.Minute))

		var rows int64
		require.NoError(t, db.Model(&entry{}).Count(&rows).Error)
		require.Zero(t, rows)

		var got payload
		require.ErrorIs(t, s.Get(t.Context(), "key", &got), ErrNotFound)
	})

	t.Run("FreshStoreReadsBackPriorWrites", func(t *testing.T) {
		s, db := open(t)
		want := payload{Name: "before-restart", N: 1}
		require.NoError(t, s.Put(t.Context(), "key", want, time.Minute))

		// A second store over the same database, as a process restart would
		// build, sees the first one's writes.
		fresh, err := New(t.Context(), db)
		require.NoError(t, err)

		var got payload
		require.NoError(t, fresh.Get(t.Context(), "key", &got))
		require.Equal(t, want, got)
	})

	t.Run("SameKeyInTwoNamespacesCoexist", func(t *testing.T) {
		s, _ := open(t)
		require.NoError(t, s.Put(t.Context(), "key", payload{Name: "a", N: 1}, time.Minute))
		b := s.Namespace("b")
		require.NoError(t, b.Put(t.Context(), "key", payload{Name: "b", N: 2}, time.Minute))

		var fromA, fromB payload
		require.NoError(t, s.Get(t.Context(), "key", &fromA))
		require.Equal(t, payload{Name: "a", N: 1}, fromA)
		require.NoError(t, b.Get(t.Context(), "key", &fromB))
		require.Equal(t, payload{Name: "b", N: 2}, fromB)
	})
}

func TestSQLiteStore(t *testing.T) {
	runStoreSuite(t, func(t *testing.T) (Store, *gorm.DB) {
		db := testutil.NewDB(t, Models())
		s, err := New(t.Context(), db)
		require.NoError(t, err)
		return s, db
	})
}

// Postgres reclaims expired rows with a trigger; SQLite has no triggers, so New
// sweeps them at startup instead. The read path filters on expiry either way,
// so this only checks that the startup pass does not take unexpired rows with
// it.
func TestNewSweepsExpiredOnSQLite(t *testing.T) {
	db := testutil.NewDB(t, Models())
	s, err := New(t.Context(), db)
	require.NoError(t, err)

	require.NoError(t, s.Put(t.Context(), "expired", payload{Name: "gone", N: 0}, -time.Second))
	require.NoError(t, s.Put(t.Context(), "live", payload{Name: "kept", N: 1}, time.Hour))

	_, err = New(t.Context(), db)
	require.NoError(t, err)

	var rows []entry
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, "live", rows[0].Key)

	var gone payload
	require.ErrorIs(t, s.Get(t.Context(), "expired", &gone), ErrNotFound)
}
