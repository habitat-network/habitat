package repo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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

	_, err = repo.PutRecord(t.Context(), "my-did", collection, key, val, nil)
	require.NoError(t, err)

	got, err := repo.GetRecord(t.Context(), "my-did", collection, key)
	require.NoError(t, err)

	var unmarshalled map[string]any
	err = json.Unmarshal([]byte(got.Value), &unmarshalled)
	require.NoError(t, err)

	require.Equal(t, val, unmarshalled)
}

func TestRepoListRecords(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, err := NewRepo(db)
	require.NoError(t, err)
	_, err = repo.PutRecord(t.Context(),
		"my-did",
		"network.habitat.collection-1",
		"key-1",
		map[string]any{"data": "value"},
		nil,
	)
	require.NoError(t, err)

	_, err = repo.PutRecord(t.Context(),
		"my-did",
		"network.habitat.collection-1",
		"key-2",
		map[string]any{"data": "value"},
		nil,
	)
	require.NoError(t, err)

	_, err = repo.PutRecord(t.Context(),
		"my-did",
		"network.habitat.collection-2",
		"key-2",
		map[string]any{"data": "value"},
		nil,
	)
	require.NoError(t, err)

	records, err := repo.ListRecords(t.Context(),
		"my-did",
		"network.habitat.collection-1",
		[]string{},
		[]string{},
	)
	require.NoError(t, err)
	require.Len(t, records, 0)

	records, err = repo.ListRecords(t.Context(),
		"my-did",
		"network.habitat.collection-1",
		[]string{"network.habitat.collection-1.key-1", "network.habitat.collection-1.key-2"},
		[]string{},
	)
	require.NoError(t, err)
	require.Len(t, records, 2)

	records, err = repo.ListRecords(t.Context(),
		"my-did",
		"network.habitat.collection-1",
		[]string{"network.habitat.collection-1.*"},
		[]string{},
	)
	require.NoError(t, err)
	require.Len(t, records, 2)

	records, err = repo.ListRecords(t.Context(),
		"my-did",
		"network.habitat.collection-1",
		[]string{"network.habitat.collection-1.*"},
		[]string{"network.habitat.collection-1.key-1"},
	)
	require.NoError(t, err)
	require.Len(t, records, 1)

	records, err = repo.ListRecords(t.Context(),
		"my-did",
		"network.habitat.collection-2",
		[]string{"network.habitat.*"},
		[]string{},
	)
	require.NoError(t, err)
	require.Len(t, records, 1)

	records, err = repo.ListRecords(t.Context(),
		"my-did",
		"network.habitat.collection-2",
		[]string{"network.habitat.*"},
		[]string{"network.habitat.collection-2.*"},
	)
	require.NoError(t, err)
	require.Len(t, records, 0)
}

func TestListRecordsByOwnersDeprecated(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, err := NewRepo(db)
	require.NoError(t, err)

	coll := "test.collection"
	val := map[string]any{"data": "value"}

	_, err = repo.PutRecord(t.Context(), "did:alice", coll, "rkey-1", val, nil)
	require.NoError(t, err)
	_, err = repo.PutRecord(t.Context(), "did:alice", coll, "rkey-2", val, nil)
	require.NoError(t, err)
	_, err = repo.PutRecord(t.Context(), "did:bob", coll, "rkey-1", val, nil)
	require.NoError(t, err)
	_, err = repo.PutRecord(t.Context(), "did:carol", coll, "rkey-1", val, nil)
	require.NoError(t, err)
	// Record in a different collection — must not appear in results
	_, err = repo.PutRecord(t.Context(), "did:alice", "other.collection", "rkey-1", val, nil)
	require.NoError(t, err)

	t.Run("returns empty for empty owner list", func(t *testing.T) {
		records, err := repo.ListRecordsByOwnersDeprecated([]string{}, coll)
		require.NoError(t, err)
		require.Empty(t, records)
	})

	t.Run("returns all records for a single owner in the collection", func(t *testing.T) {
		records, err := repo.ListRecordsByOwnersDeprecated([]string{"did:alice"}, coll)
		require.NoError(t, err)
		require.Len(t, records, 2)
		for _, r := range records {
			require.Equal(t, "did:alice", r.Did)
			require.Equal(t, coll, r.Collection)
		}
	})

	t.Run("returns records across multiple owners", func(t *testing.T) {
		records, err := repo.ListRecordsByOwnersDeprecated([]string{"did:alice", "did:bob"}, coll)
		require.NoError(t, err)
		require.Len(t, records, 3)
	})

	t.Run("does not include records from other collections", func(t *testing.T) {
		records, err := repo.ListRecordsByOwnersDeprecated([]string{"did:alice"}, coll)
		require.NoError(t, err)
		for _, r := range records {
			require.Equal(t, coll, r.Collection)
		}
	})

	t.Run("returns empty for owner with no records in the collection", func(t *testing.T) {
		records, err := repo.ListRecordsByOwnersDeprecated([]string{"did:unknown"}, coll)
		require.NoError(t, err)
		require.Empty(t, records)
	})
}

func TestListSpecificRecordsDeprecated(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, err := NewRepo(db)
	require.NoError(t, err)

	coll := "test.collection"
	val := map[string]any{"data": "value"}

	_, err = repo.PutRecord(t.Context(), "did:alice", coll, "rkey-1", val, nil)
	require.NoError(t, err)
	_, err = repo.PutRecord(t.Context(), "did:alice", coll, "rkey-2", val, nil)
	require.NoError(t, err)
	_, err = repo.PutRecord(t.Context(), "did:bob", coll, "rkey-1", val, nil)
	require.NoError(t, err)
	_, err = repo.PutRecord(t.Context(), "did:bob", coll, "rkey-2", val, nil)
	require.NoError(t, err)
	// Record in a different collection — must not appear in results
	_, err = repo.PutRecord(t.Context(), "did:alice", "other.collection", "rkey-1", val, nil)
	require.NoError(t, err)

	type pair = struct{ Owner, Rkey string }

	t.Run("returns empty for empty pairs list", func(t *testing.T) {
		records, err := repo.ListSpecificRecordsDeprecated(coll, []pair{})
		require.NoError(t, err)
		require.Empty(t, records)
	})

	t.Run("returns a single specific record", func(t *testing.T) {
		records, err := repo.ListSpecificRecordsDeprecated(coll, []pair{{"did:alice", "rkey-1"}})
		require.NoError(t, err)
		require.Len(t, records, 1)
		require.Equal(t, "did:alice", records[0].Did)
		require.Equal(t, "rkey-1", records[0].Rkey)
	})

	t.Run("returns specific records across multiple owners", func(t *testing.T) {
		records, err := repo.ListSpecificRecordsDeprecated(coll, []pair{
			{"did:alice", "rkey-1"},
			{"did:bob", "rkey-2"},
		})
		require.NoError(t, err)
		require.Len(t, records, 2)
	})

	t.Run("does not return other rkeys for the same owner", func(t *testing.T) {
		records, err := repo.ListSpecificRecordsDeprecated(coll, []pair{{"did:alice", "rkey-1"}})
		require.NoError(t, err)
		require.Len(t, records, 1)
		require.Equal(t, "rkey-1", records[0].Rkey)
	})

	t.Run("does not include records from other collections", func(t *testing.T) {
		records, err := repo.ListSpecificRecordsDeprecated(coll, []pair{{"did:alice", "rkey-1"}})
		require.NoError(t, err)
		for _, r := range records {
			require.Equal(t, coll, r.Collection)
		}
	})

	t.Run("returns empty for non-existent pair", func(t *testing.T) {
		records, err := repo.ListSpecificRecordsDeprecated(coll, []pair{{"did:unknown", "rkey-1"}})
		require.NoError(t, err)
		require.Empty(t, records)
	})
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
	_, _, err = repo.GetBlob(t.Context(), did, "bafkreiaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	require.ErrorIs(t, err, ErrRecordNotFound)
}
