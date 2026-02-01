package privi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/habitat-network/habitat/api/habitat"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestHasRepoForDid(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, err := NewSQLiteRepo(db)
	require.NoError(t, err)

	// No records exist yet, should return false
	has, err := repo.HasRepoForDid("did:plc:alice123")
	require.NoError(t, err)
	require.False(t, has)

	// Add a record for the DID
	err = repo.PutRecord("did:plc:alice123", "test-key", map[string]any{"data": "value"}, nil)
	require.NoError(t, err)

	// Now the DID should be found
	has, err = repo.HasRepoForDid("did:plc:alice123")
	require.NoError(t, err)
	require.True(t, has)

	// A different DID should still return false
	has, err = repo.HasRepoForDid("did:plc:bob456")
	require.NoError(t, err)
	require.False(t, has)
}

func TestSQLiteRepoPutAndGetRecord(t *testing.T) {
	testDBPath := filepath.Join(os.TempDir(), "test_privi.db")
	defer func() { require.NoError(t, os.Remove(testDBPath)) }()

	priviDB, err := gorm.Open(sqlite.Open(testDBPath), &gorm.Config{})
	require.NoError(t, err)

	repo, err := NewSQLiteRepo(priviDB)
	require.NoError(t, err)

	key := "test-key"
	val := map[string]any{"data": "value", "data-1": float64(123), "data-2": true}
	ownerDID := "did:plc:owner123"

	err = repo.PutRecord(ownerDID, key, val, nil)
	require.NoError(t, err)

	got, err := repo.GetRecord(ownerDID, key)
	require.NoError(t, err)

	var unmarshalled map[string]any
	err = json.Unmarshal([]byte(got.Rec), &unmarshalled)
	require.NoError(t, err)

	require.Equal(t, val, unmarshalled)
}

func TestSQLiteRepoListRecords(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	repo, err := NewSQLiteRepo(db)
	require.NoError(t, err)
	ownerDID := "did:plc:owner123"
	err = repo.PutRecord(
		ownerDID,
		"network.habitat.collection-1.key-1",
		map[string]any{"data": "value"},
		nil,
	)
	require.NoError(t, err)

	err = repo.PutRecord(
		ownerDID,
		"network.habitat.collection-1.key-2",
		map[string]any{"data": "value"},
		nil,
	)
	require.NoError(t, err)

	err = repo.PutRecord(
		ownerDID,
		"network.habitat.collection-2.key-2",
		map[string]any{"data": "value"},
		nil,
	)
	require.NoError(t, err)

	records, err := repo.ListRecords(
		&habitat.NetworkHabitatRepoListRecordsParams{
			Repo:       ownerDID,
			Collection: "network.habitat.collection-1",
		},
		[]string{},
		[]string{},
	)
	require.NoError(t, err)
	require.Len(t, records, 0)

	records, err = repo.ListRecords(
		&habitat.NetworkHabitatRepoListRecordsParams{
			Repo:       ownerDID,
			Collection: "network.habitat.collection-1",
		},
		[]string{"network.habitat.collection-1.key-1", "network.habitat.collection-1.key-2"},
		[]string{},
	)
	require.NoError(t, err)
	require.Len(t, records, 2)

	records, err = repo.ListRecords(
		&habitat.NetworkHabitatRepoListRecordsParams{
			Repo:       ownerDID,
			Collection: "network.habitat.collection-1",
		},
		[]string{"network.habitat.collection-1.*"},
		[]string{},
	)
	require.NoError(t, err)
	require.Len(t, records, 2)

	records, err = repo.ListRecords(
		&habitat.NetworkHabitatRepoListRecordsParams{
			Repo:       ownerDID,
			Collection: "network.habitat.collection-1",
		},
		[]string{"network.habitat.collection-1.*"},
		[]string{"network.habitat.collection-1.key-1"},
	)
	require.NoError(t, err)
	require.Len(t, records, 1)

	records, err = repo.ListRecords(
		&habitat.NetworkHabitatRepoListRecordsParams{
			Repo:       ownerDID,
			Collection: "network.habitat.collection-2",
		},
		[]string{"network.habitat.*"},
		[]string{},
	)
	require.NoError(t, err)
	require.Len(t, records, 1)

	records, err = repo.ListRecords(
		&habitat.NetworkHabitatRepoListRecordsParams{
			Repo:       ownerDID,
			Collection: "network.habitat.collection-2",
		},
		[]string{"network.habitat.*"},
		[]string{"network.habitat.collection-2.*"},
	)
	require.NoError(t, err)
	require.Len(t, records, 0)
}

func TestUploadAndGetBlob(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, err := NewSQLiteRepo(db)
	require.NoError(t, err)

	did := "did:plc:alice123"
	// use an empty blob to avoid hitting sqlite3.SQLITE_LIMIT_LENGTH in test environment
	blob := []byte("this is my test blob")
	mtype := "text/plain"

	bmeta, err := repo.UploadBlob(did, blob, mtype)
	require.NoError(t, err)
	require.NotNil(t, bmeta)
	require.Equal(t, mtype, bmeta.MimeType)
	require.Equal(t, int64(len(blob)), bmeta.Size)

	m, gotBlob, err := repo.GetBlob(did, bmeta.Ref.String())
	require.NoError(t, err)
	require.Equal(t, mtype, m)
	require.Equal(t, blob, gotBlob)
}
