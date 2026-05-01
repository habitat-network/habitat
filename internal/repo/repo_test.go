package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/permissions"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRepoPutAndGetRecord(t *testing.T) {
	testDBPath := filepath.Join(os.TempDir(), "test_pear.db")
	defer func() { require.NoError(t, os.Remove(testDBPath)) }()

	pearDB, err := gorm.Open(sqlite.Open(testDBPath), &gorm.Config{})
	require.NoError(t, err)

	repo, _, err := NewRepo(t.Context(), pearDB)
	require.NoError(t, err)

	collection := "test.collection"
	key := "test-key"
	val := map[string]any{"data": "value", "data-1": float64(123), "data-2": true}

	_, err = repo.PutRecord(t.Context(), Record{
		Did:        "my-did",
		Collection: collection,
		Rkey:       key,
		Value:      val,
	}, nil)
	require.NoError(t, err)

	// Put again to test on conflict works
	_, err = repo.PutRecord(t.Context(), Record{
		Did:        "my-did",
		Collection: collection,
		Rkey:       key,
		Value:      val,
	}, nil)
	require.NoError(t, err)

	got, err := repo.GetRecord(t.Context(), "my-did", collection, key)
	require.NoError(t, err)

	require.Equal(t, val, got.Value)
}

func TestRepoListRecords(t *testing.T) {
	ctx := t.Context()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, _, err := NewRepo(t.Context(), db)
	require.NoError(t, err)
	_, err = repo.PutRecord(
		t.Context(),
		Record{
			"my-did",
			"network.habitat.collection-1",
			"key-1",
			map[string]any{"data": "value"},
		},
		nil,
	)
	require.NoError(t, err)

	_, err = repo.PutRecord(
		ctx,
		Record{
			"my-did",
			"network.habitat.collection-1",
			"key-2",
			map[string]any{"data": "value"},
		},
		nil,
	)
	require.NoError(t, err)

	_, err = repo.PutRecord(
		ctx,
		Record{
			"my-did",
			"network.habitat.collection-2",
			"key-2",
			map[string]any{"data": "value"},
		},
		nil,
	)
	require.NoError(t, err)

	records, err := repo.ListRecordsFromPermissions(ctx, nil)
	require.NoError(t, err)
	require.Len(t, records, 0)

	records, err = repo.ListRecordsFromPermissions(
		ctx,
		[]permissions.Permission{
			{
				Owner:      "my-did",
				Collection: "network.habitat.collection-1",
				Rkey:       "key-1",
			},
			{
				Owner:      "my-did",
				Collection: "network.habitat.collection-1",
				Rkey:       "key-2",
			},
		},
	)
	require.NoError(t, err)
	require.Len(t, records, 2)
}

func TestRepoListCollections(t *testing.T) {
	ctx := t.Context()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, _, err := NewRepo(t.Context(), db)
	require.NoError(t, err)

	did := syntax.DID("did:plc:testuser")

	// No collections yet
	collections, err := repo.ListCollections(ctx, did)
	require.NoError(t, err)
	require.Empty(t, collections)

	// Add records across two collections
	for _, rec := range []Record{
		{Did: string(did), Collection: "network.habitat.alpha", Rkey: "key-1", Value: map[string]any{"x": "1"}},
		{Did: string(did), Collection: "network.habitat.alpha", Rkey: "key-2", Value: map[string]any{"x": "2"}},
		{Did: string(did), Collection: "network.habitat.beta", Rkey: "key-1", Value: map[string]any{"x": "3"}},
	} {
		_, err = repo.PutRecord(ctx, rec, nil)
		require.NoError(t, err)
	}

	collections, err = repo.ListCollections(ctx, did)
	require.NoError(t, err)
	require.Len(t, collections, 2)

	byName := map[string]CollectionMetadata{}
	for _, c := range collections {
		byName[c.Name] = c
	}

	require.Equal(t, 2, byName["network.habitat.alpha"].RecordCount)
	require.Equal(t, 1, byName["network.habitat.beta"].RecordCount)

	otherDID := syntax.DID("did:plc:other")
	// Records for a different DID are not included
	_, err = repo.PutRecord(ctx, Record{
		Did:        otherDID.String(),
		Collection: "network.habitat.otherCollection",
		Rkey:       "key-1",
		Value:      map[string]any{"x": "other"},
	}, nil)
	require.NoError(t, err)

	collections, err = repo.ListCollections(ctx, did)
	require.NoError(t, err)
	require.Len(t, collections, 2)

	collections, err = repo.ListCollections(ctx, otherDID)
	require.NoError(t, err)
	require.Len(t, collections, 1)

}

func TestRepoUploadAndGetBlob(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, _, err := NewRepo(t.Context(), db)
	require.NoError(t, err)

	did := "did:plc:testuser"
	data := []byte("hello blob world")
	mimeType := "text/plain"

	// Upload a blob
	ref, err := repo.UploadBlob(t.Context(), did, data, mimeType)
	require.NoError(t, err)
	require.NotNil(t, ref)
	require.Equal(t, mimeType, ref.MimeType)
	require.Equal(t, int64(len(data)), ref.Size)

	// Retrieve it by CID
	gotMime, gotData, err := repo.GetBlob(t.Context(), did, ref.Ref.String())
	require.NoError(t, err)
	require.Equal(t, mimeType, gotMime)
	require.Equal(t, data, gotData)

	// Upload a second blob with a different mime type
	data2 := []byte{0x89, 0x50, 0x4E, 0x47} // fake PNG header
	ref2, err := repo.UploadBlob(t.Context(), did, data2, "image/png")
	require.NoError(t, err)
	require.Equal(t, "image/png", ref2.MimeType)
	require.Equal(t, int64(len(data2)), ref2.Size)

	// Both blobs should be independently retrievable
	gotMime2, gotData2, err := repo.GetBlob(t.Context(), did, ref2.Ref.String())
	require.NoError(t, err)
	require.Equal(t, "image/png", gotMime2)
	require.Equal(t, data2, gotData2)

	// Original blob is still intact
	gotMime, gotData, err = repo.GetBlob(t.Context(), did, ref.Ref.String())
	require.NoError(t, err)
	require.Equal(t, mimeType, gotMime)
	require.Equal(t, data, gotData)

	// Getting a non-existent blob returns an error
	_, _, err = repo.GetBlob(
		t.Context(),
		did,
		"bafkreiaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	)
	require.ErrorIs(t, err, ErrRecordNotFound)
}

func TestListRecords(t *testing.T) {
	ctx := t.Context()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, _, err := NewRepo(t.Context(), db)
	require.NoError(t, err)

	did := "did:plc:testuser"
	coll1 := "network.habitat.alpha"
	coll2 := "network.habitat.beta"

	for _, rec := range []Record{
		{Did: did, Collection: coll1, Rkey: "key-1", Value: map[string]any{"x": "1"}},
		{Did: did, Collection: coll1, Rkey: "key-2", Value: map[string]any{"x": "2"}},
		{Did: did, Collection: coll2, Rkey: "key-1", Value: map[string]any{"x": "3"}},
	} {
		_, err = repo.PutRecord(ctx, rec, nil)
		require.NoError(t, err)
	}

	// Returns only records in the specified collection
	records, err := repo.ListRecords(ctx, did, coll1)
	require.NoError(t, err)
	require.Len(t, records, 2)
	for _, r := range records {
		require.Equal(t, did, r.Did)
		require.Equal(t, coll1, r.Collection)
	}

	// Returns records for the other collection
	records, err = repo.ListRecords(ctx, did, coll2)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "key-1", records[0].Rkey)

	// Returns empty for a non-existent collection
	records, err = repo.ListRecords(ctx, did, "network.habitat.nonexistent")
	require.NoError(t, err)
	require.Empty(t, records)

	// Returns empty for a different DID
	records, err = repo.ListRecords(ctx, "did:plc:other", coll1)
	require.NoError(t, err)
	require.Empty(t, records)
}

// TestPutRecordOnConflict verifies both OnConflict clauses inside PutRecord:
//  1. record rows use UpdateAll — a second put with the same (did, collection, rkey)
//     must overwrite the value, not error.
//  2. link rows use DoNothing — putting the same blob-referencing record twice must
//     not produce a duplicate-key error or a duplicate link row.
func TestPutRecordOnConflict(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	require.NoError(t, err)

	repo, _, err := NewRepo(t.Context(), db)
	require.NoError(t, err)

	ctx := t.Context()
	did := "did:plc:onconflict"
	collection := "network.habitat.test"
	rkey := "rkey-1"

	// A valid atproto blob reference embedded in the record value.
	// atdata.ExtractBlobs will pick this up and create a link row.
	blobCID := "bafkreihdwdcefgh4dqkjv67uzcmw37nwqfknnagwmjb44agkh4lphqzgxq"
	recordWithBlob := map[string]any{
		"$type": "network.habitat.test",
		"image": map[string]any{
			"$type":    "blob",
			"ref":      map[string]any{"$link": blobCID},
			"mimeType": "image/png",
			"size":     float64(42),
		},
	}

	t.Run("record OnConflict UpdateAll: second put overwrites value", func(t *testing.T) {
		first := map[string]any{"msg": "hello"}
		_, err := repo.PutRecord(ctx, Record{Did: did, Collection: collection, Rkey: rkey, Value: first}, nil)
		require.NoError(t, err)

		second := map[string]any{"msg": "world"}
		_, err = repo.PutRecord(ctx, Record{Did: did, Collection: collection, Rkey: rkey, Value: second}, nil)
		require.NoError(t, err)

		got, err := repo.GetRecord(ctx, did, collection, rkey)
		require.NoError(t, err)
		require.Equal(t, "world", got.Value["msg"], "value should have been updated by OnConflict UpdateAll")
	})

	t.Run("link OnConflict DoNothing: duplicate blob ref does not error or duplicate", func(t *testing.T) {
		// First put — inserts the record row and the link row.
		_, err := repo.PutRecord(ctx, Record{Did: did, Collection: collection, Rkey: rkey + "-blob", Value: recordWithBlob}, nil)
		require.NoError(t, err)

		// Second put — record row conflicts (UpdateAll), link row conflicts (DoNothing).
		// Neither should return an error.
		_, err = repo.PutRecord(ctx, Record{Did: did, Collection: collection, Rkey: rkey + "-blob", Value: recordWithBlob}, nil)
		require.NoError(t, err)

		// Confirm exactly one link row exists for the blob CID.
		links, err := repo.GetBlobLinks(ctx, syntax.CID(blobCID), syntax.DID(did))
		require.NoError(t, err)
		require.Len(t, links, 1, "DoNothing should prevent duplicate link rows")
	})
}

func TestCreateRecord(t *testing.T) {
	ctx := t.Context()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, _, err := NewRepo(t.Context(), db)
	require.NoError(t, err)

	did := "did:plc:testuser"
	collection := "network.habitat.test"
	val := map[string]any{"msg": "hello"}

	t.Run("creates a new record and returns the correct URI", func(t *testing.T) {
		uri, err := repo.CreateRecord(ctx, Record{Did: did, Collection: collection, Rkey: "rkey-1", Value: val}, nil)
		require.NoError(t, err)
		require.NotEmpty(t, uri)

		got, err := repo.GetRecord(ctx, did, collection, "rkey-1")
		require.NoError(t, err)
		require.Equal(t, val, got.Value)
	})

	t.Run("errors when record already exists", func(t *testing.T) {
		_, err := repo.CreateRecord(ctx, Record{Did: did, Collection: collection, Rkey: "rkey-2", Value: val}, nil)
		require.NoError(t, err)

		_, err = repo.CreateRecord(ctx, Record{Did: did, Collection: collection, Rkey: "rkey-2", Value: val}, nil)
		require.ErrorIs(t, err, ErrRecordAlreadyCreated)
	})

	t.Run("different rkeys in same collection are independent", func(t *testing.T) {
		_, err := repo.CreateRecord(ctx, Record{Did: did, Collection: collection, Rkey: "rkey-a", Value: map[string]any{"n": "a"}}, nil)
		require.NoError(t, err)

		_, err = repo.CreateRecord(ctx, Record{Did: did, Collection: collection, Rkey: "rkey-b", Value: map[string]any{"n": "b"}}, nil)
		require.NoError(t, err)
	})
}

func TestDeleteRecord(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, _, err := NewRepo(t.Context(), db)
	require.NoError(t, err)

	ownerDID := syntax.DID("did:example:owner")

	coll := syntax.NSID("my.fake.collection")
	rkey := syntax.RecordKey("my-rkey")
	validate := true
	val := map[string]any{"key": "val"}

	_, err = repo.PutRecord(t.Context(), Record{
		Did:        string(ownerDID),
		Collection: coll.String(),
		Rkey:       rkey.String(),
		Value:      val,
	}, &validate)
	require.NoError(t, err)
	t.Run("basic delete", func(t *testing.T) {
		err := repo.DeleteRecord(t.Context(), ownerDID.String(), coll.String(), rkey.String())
		require.NoError(t, err)
	})

	t.Run("deleting record that doesn't exist is non-error and no-op", func(t *testing.T) {
		err := repo.DeleteRecord(t.Context(), ownerDID.String(), coll.String(), "some-key")
		require.NoError(t, err)
	})
}
