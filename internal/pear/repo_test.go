package pear

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

	err = repo.putRecord("my-did", collection, key, val, nil)
	require.NoError(t, err)

	got, err := repo.getRecord("my-did", collection, key)
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
	err = repo.putRecord(
		"my-did",
		"network.habitat.collection-1",
		"key-1",
		map[string]any{"data": "value"},
		nil,
	)
	require.NoError(t, err)

	err = repo.putRecord(
		"my-did",
		"network.habitat.collection-1",
		"key-2",
		map[string]any{"data": "value"},
		nil,
	)
	require.NoError(t, err)

	err = repo.putRecord(
		"my-did",
		"network.habitat.collection-2",
		"key-2",
		map[string]any{"data": "value"},
		nil,
	)
	require.NoError(t, err)

	records, err := repo.listRecords(
		"my-did",
		"network.habitat.collection-1",
		[]string{},
		[]string{},
	)
	require.NoError(t, err)
	require.Len(t, records, 0)

	records, err = repo.listRecords(
		"my-did",
		"network.habitat.collection-1",
		[]string{"network.habitat.collection-1.key-1", "network.habitat.collection-1.key-2"},
		[]string{},
	)
	require.NoError(t, err)
	require.Len(t, records, 2)

	records, err = repo.listRecords(
		"my-did",
		"network.habitat.collection-1",
		[]string{"network.habitat.collection-1.*"},
		[]string{},
	)
	require.NoError(t, err)
	require.Len(t, records, 2)

	records, err = repo.listRecords(
		"my-did",
		"network.habitat.collection-1",
		[]string{"network.habitat.collection-1.*"},
		[]string{"network.habitat.collection-1.key-1"},
	)
	require.NoError(t, err)
	require.Len(t, records, 1)

	records, err = repo.listRecords(
		"my-did",
		"network.habitat.collection-2",
		[]string{"network.habitat.*"},
		[]string{},
	)
	require.NoError(t, err)
	require.Len(t, records, 1)

	records, err = repo.listRecords(
		"my-did",
		"network.habitat.collection-2",
		[]string{"network.habitat.*"},
		[]string{"network.habitat.collection-2.*"},
	)
	require.NoError(t, err)
	require.Len(t, records, 0)
}

func TestCliqueAddAndGetItems(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, err := NewRepo(db)
	require.NoError(t, err)

	cliqueURI := "habitat://did:plc:owner123/network.habitat.clique/clique1"
	item1 := "habitat://did:plc:owner123/network.habitat.post/post1"
	item2 := "habitat://did:plc:owner123/network.habitat.post/post2"

	// Empty clique returns no items
	items, err := repo.getCliqueItems(cliqueURI)
	require.NoError(t, err)
	require.Empty(t, items)

	// Add items
	require.NoError(t, repo.addCliqueItem(cliqueURI, item1))
	require.NoError(t, repo.addCliqueItem(cliqueURI, item2))

	// Get items
	items, err = repo.getCliqueItems(cliqueURI)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Contains(t, items, item1)
	require.Contains(t, items, item2)

	// Adding duplicate is a no-op
	require.NoError(t, repo.addCliqueItem(cliqueURI, item1))
	items, err = repo.getCliqueItems(cliqueURI)
	require.NoError(t, err)
	require.Len(t, items, 2)

	// Different clique is isolated
	otherClique := "habitat://did:plc:owner123/network.habitat.clique/clique2"
	items, err = repo.getCliqueItems(otherClique)
	require.NoError(t, err)
	require.Empty(t, items)
}

func TestUploadAndGetBlob(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, err := NewRepo(db)
	require.NoError(t, err)

	did := "did:example:alice"
	// use an empty blob to avoid hitting sqlite3.SQLITE_LIMIT_LENGTH in test environment
	blob := []byte("this is my test blob")
	mtype := "text/plain"

	bmeta, err := repo.uploadBlob(did, blob, mtype)
	require.NoError(t, err)
	require.NotNil(t, bmeta)
	require.Equal(t, mtype, bmeta.MimeType)
	require.Equal(t, int64(len(blob)), bmeta.Size)

	m, gotBlob, err := repo.getBlob(did, bmeta.Ref.String())
	require.NoError(t, err)
	require.Equal(t, mtype, m)
	require.Equal(t, blob, gotBlob)
}
