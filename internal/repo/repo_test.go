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
)

func TestRepoPutAndGetRecord(t *testing.T) {
	testDBPath := filepath.Join(os.TempDir(), "test_pear.db")
	defer func() { require.NoError(t, os.Remove(testDBPath)) }()

	pearDB, err := gorm.Open(sqlite.Open(testDBPath), &gorm.Config{})
	require.NoError(t, err)

	repo, err := NewRepo(pearDB)
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

	got, err := repo.GetRecord(t.Context(), "my-did", collection, key)
	require.NoError(t, err)

	require.Equal(t, val, got.Value)
}

func TestRepoListRecords(t *testing.T) {
	ctx := t.Context()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, err := NewRepo(db)
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

	repo, err := NewRepo(db)
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

	repo, err := NewRepo(db)
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

	repo, err := NewRepo(db)
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

func TestDeleteRecord(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, err := NewRepo(db)
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
