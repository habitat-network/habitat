package repo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
	ctx := t.Context()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, err := NewRepo(db)
	require.NoError(t, err)
	_, err = repo.PutRecord(
		t.Context(),
		"my-did",
		"network.habitat.collection-1",
		"key-1",
		map[string]any{"data": "value"},
		nil,
	)
	require.NoError(t, err)

	_, err = repo.PutRecord(
		ctx,
		"my-did",
		"network.habitat.collection-1",
		"key-2",
		map[string]any{"data": "value"},
		nil,
	)
	require.NoError(t, err)

	_, err = repo.PutRecord(
		ctx,
		"my-did",
		"network.habitat.collection-2",
		"key-2",
		map[string]any{"data": "value"},
		nil,
	)
	require.NoError(t, err)

	records, err := repo.ListRecords(ctx, nil)
	require.NoError(t, err)
	require.Len(t, records, 0)

	records, err = repo.ListRecords(
		ctx,
		[]permissions.Permission{
			{
				Owner:      "my-did",
				Collection: "network.habitat.collection-1",
				Rkey:       "key-1",
				Effect:     "allow",
			},
			{
				Owner:      "my-did",
				Collection: "network.habitat.collection-1",
				Rkey:       "key-2",
				Effect:     "allow",
			},
		},
	)
	require.NoError(t, err)
	require.Len(t, records, 2)

	records, err = repo.ListRecords(
		ctx,
		[]permissions.Permission{
			{
				Owner:      "my-did",
				Collection: "network.habitat.collection-1",
				Effect:     "allow",
			},
		},
	)
	require.NoError(t, err)
	require.Len(t, records, 2)

	records, err = repo.ListRecords(
		ctx,
		[]permissions.Permission{
			{
				Owner:      "my-did",
				Collection: "network.habitat.collection-1",
				Effect:     "allow",
			},
			{
				Owner:      "my-did",
				Collection: "network.habitat.collection-1",
				Rkey:       "key-1",
				Effect:     "deny",
			},
		},
	)
	require.NoError(t, err)
	require.Len(t, records, 1)
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
