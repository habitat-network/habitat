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

func TestRepoListRecordsByOwnersEmptyList(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, err := NewRepo(db)
	require.NoError(t, err)

	collection := "network.habitat.posts"
	records, err := repo.listRecordsByOwners([]string{}, collection)
	require.NoError(t, err)
	require.Empty(t, records)
}

func TestRepoListRecordsByOwnersSingleOwner(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, err := NewRepo(db)
	require.NoError(t, err)

	collection := "network.habitat.posts"
	require.NoError(t, repo.putRecord("alice-did", collection, "alice-rkey1", map[string]any{"data": "alice1"}, nil))
	require.NoError(t, repo.putRecord("alice-did", collection, "alice-rkey2", map[string]any{"data": "alice2"}, nil))

	records, err := repo.listRecordsByOwners([]string{"alice-did"}, collection)
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, "alice-did", records[0].Did)
	require.Equal(t, "alice-did", records[1].Did)
	require.Equal(t, "alice-rkey1", records[0].Rkey)
	require.Equal(t, "alice-rkey2", records[1].Rkey)
}

func TestRepoListRecordsByOwnersMultipleOwners(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, err := NewRepo(db)
	require.NoError(t, err)

	collection := "network.habitat.posts"
	require.NoError(t, repo.putRecord("alice-did", collection, "alice-rkey1", map[string]any{"data": "alice1"}, nil))
	require.NoError(t, repo.putRecord("alice-did", collection, "alice-rkey2", map[string]any{"data": "alice2"}, nil))
	require.NoError(t, repo.putRecord("bob-did", collection, "bob-rkey1", map[string]any{"data": "bob1"}, nil))
	require.NoError(t, repo.putRecord("bob-did", collection, "bob-rkey2", map[string]any{"data": "bob2"}, nil))

	records, err := repo.listRecordsByOwners([]string{"alice-did", "bob-did"}, collection)
	require.NoError(t, err)
	require.Len(t, records, 4)
	// Should be ordered by did ASC, rkey ASC
	require.Equal(t, "alice-did", records[0].Did)
	require.Equal(t, "alice-did", records[1].Did)
	require.Equal(t, "bob-did", records[2].Did)
	require.Equal(t, "bob-did", records[3].Did)
}

func TestRepoListRecordsByOwnersFiltersByCollection(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, err := NewRepo(db)
	require.NoError(t, err)

	collection := "network.habitat.posts"
	otherCollection := "network.habitat.likes"
	require.NoError(t, repo.putRecord("alice-did", collection, "alice-rkey1", map[string]any{"data": "alice1"}, nil))
	require.NoError(t, repo.putRecord("alice-did", collection, "alice-rkey2", map[string]any{"data": "alice2"}, nil))
	require.NoError(t, repo.putRecord("alice-did", otherCollection, "alice-like1", map[string]any{"data": "like1"}, nil))

	records, err := repo.listRecordsByOwners([]string{"alice-did"}, collection)
	require.NoError(t, err)
	require.Len(t, records, 2)
	// Should not include records from other collection
	for _, record := range records {
		require.Equal(t, collection, record.Collection)
	}
}

func TestRepoListRecordsByOwnersNonExistentOwners(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, err := NewRepo(db)
	require.NoError(t, err)

	collection := "network.habitat.posts"
	records, err := repo.listRecordsByOwners([]string{"nonexistent-did"}, collection)
	require.NoError(t, err)
	require.Empty(t, records)
}

func TestRepoListSpecificRecordsEmptyPairs(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, err := NewRepo(db)
	require.NoError(t, err)

	collection := "network.habitat.posts"
	records, err := repo.listSpecificRecords(collection, []struct {
		Owner string
		Rkey  string
	}{})
	require.NoError(t, err)
	require.Empty(t, records)
}

func TestRepoListSpecificRecordsSingleRecord(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, err := NewRepo(db)
	require.NoError(t, err)

	collection := "network.habitat.posts"
	require.NoError(t, repo.putRecord("alice-did", collection, "rkey1", map[string]any{"data": "alice1"}, nil))

	recordPairs := []struct {
		Owner string
		Rkey  string
	}{
		{Owner: "alice-did", Rkey: "rkey1"},
	}
	records, err := repo.listSpecificRecords(collection, recordPairs)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "alice-did", records[0].Did)
	require.Equal(t, "rkey1", records[0].Rkey)
}

func TestRepoListSpecificRecordsMultipleFromSameOwner(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, err := NewRepo(db)
	require.NoError(t, err)

	collection := "network.habitat.posts"
	require.NoError(t, repo.putRecord("alice-did", collection, "rkey1", map[string]any{"data": "alice1"}, nil))
	require.NoError(t, repo.putRecord("alice-did", collection, "rkey2", map[string]any{"data": "alice2"}, nil))

	recordPairs := []struct {
		Owner string
		Rkey  string
	}{
		{Owner: "alice-did", Rkey: "rkey1"},
		{Owner: "alice-did", Rkey: "rkey2"},
	}
	records, err := repo.listSpecificRecords(collection, recordPairs)
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, "alice-did", records[0].Did)
	require.Equal(t, "alice-did", records[1].Did)
	require.Equal(t, "rkey1", records[0].Rkey)
	require.Equal(t, "rkey2", records[1].Rkey)
}

func TestRepoListSpecificRecordsMultipleOwners(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, err := NewRepo(db)
	require.NoError(t, err)

	collection := "network.habitat.posts"
	require.NoError(t, repo.putRecord("alice-did", collection, "rkey1", map[string]any{"data": "alice1"}, nil))
	require.NoError(t, repo.putRecord("bob-did", collection, "rkey2", map[string]any{"data": "bob2"}, nil))
	require.NoError(t, repo.putRecord("charlie-did", collection, "rkey1", map[string]any{"data": "charlie1"}, nil))

	recordPairs := []struct {
		Owner string
		Rkey  string
	}{
		{Owner: "alice-did", Rkey: "rkey1"},
		{Owner: "bob-did", Rkey: "rkey2"},
		{Owner: "charlie-did", Rkey: "rkey1"},
	}
	records, err := repo.listSpecificRecords(collection, recordPairs)
	require.NoError(t, err)
	require.Len(t, records, 3)
	// Should be ordered by did ASC, rkey ASC
	require.Equal(t, "alice-did", records[0].Did)
	require.Equal(t, "bob-did", records[1].Did)
	require.Equal(t, "charlie-did", records[2].Did)
}

func TestRepoListSpecificRecordsFiltersByCollection(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, err := NewRepo(db)
	require.NoError(t, err)

	collection := "network.habitat.posts"
	otherCollection := "network.habitat.likes"
	require.NoError(t, repo.putRecord("alice-did", collection, "rkey1", map[string]any{"data": "alice1"}, nil))
	require.NoError(t, repo.putRecord("alice-did", otherCollection, "rkey1", map[string]any{"data": "like1"}, nil))

	recordPairs := []struct {
		Owner string
		Rkey  string
	}{
		{Owner: "alice-did", Rkey: "rkey1"},
	}
	records, err := repo.listSpecificRecords(collection, recordPairs)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, collection, records[0].Collection)
	require.NotEqual(t, otherCollection, records[0].Collection)
}

func TestRepoListSpecificRecordsNonExistent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, err := NewRepo(db)
	require.NoError(t, err)

	collection := "network.habitat.posts"
	recordPairs := []struct {
		Owner string
		Rkey  string
	}{
		{Owner: "nonexistent-did", Rkey: "rkey1"},
	}
	records, err := repo.listSpecificRecords(collection, recordPairs)
	require.NoError(t, err)
	require.Empty(t, records)
}

func TestRepoListSpecificRecordsPartialMatches(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	repo, err := NewRepo(db)
	require.NoError(t, err)

	collection := "network.habitat.posts"
	require.NoError(t, repo.putRecord("alice-did", collection, "rkey1", map[string]any{"data": "alice1"}, nil))

	recordPairs := []struct {
		Owner string
		Rkey  string
	}{
		{Owner: "alice-did", Rkey: "rkey1"},
		{Owner: "alice-did", Rkey: "nonexistent-rkey"},
		{Owner: "nonexistent-did", Rkey: "rkey1"},
	}
	records, err := repo.listSpecificRecords(collection, recordPairs)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "alice-did", records[0].Did)
	require.Equal(t, "rkey1", records[0].Rkey)
}
