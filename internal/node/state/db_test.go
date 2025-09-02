package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewHabitatDB(t *testing.T) {
	// Setup temp file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "testdb.json")

	db, err := NewHabitatDB(tmpFile, nil)
	require.NoError(t, err)

	// Check file exists
	if _, err := os.Stat(tmpFile); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}

	// Check Bytes
	b, err := db.Bytes()
	require.NoError(t, err)

	empty, err := Schema.EmptyState()
	require.NoError(t, err)
	bytes, err := empty.Bytes()
	require.NoError(t, err)

	require.Equal(t, string(bytes), string(b), "empty state")
}

func TestNewHabitatDBExists(t *testing.T) {
	// Setup temp file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "testdb.json")

	empty, err := Schema.EmptyState()
	require.NoError(t, err)
	data, err := empty.Bytes()
	require.NoError(t, err)

	err = os.WriteFile(tmpFile, data, 0600)
	require.NoError(t, err)

	// Check file exists
	if _, err := os.Stat(tmpFile); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}

	db, err := NewHabitatDB(tmpFile, nil)
	require.NoError(t, err)

	// Check Bytes
	b, err := db.Bytes()
	require.NoError(t, err)
	require.Equal(t, string(b), string(data), "same state as before")
}

func TestProposeTransitions(t *testing.T) {
	// Setup temp file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "testdb.json")

	db, err := NewHabitatDB(tmpFile, nil)
	require.NoError(t, err)

	// Check file exists
	if _, err := os.Stat(tmpFile); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}

	_, err = db.ProposeTransitions([]Transition{
		&addUserTransition{
			User: &User{
				Username: "user1",
				ID:       "id1",
				DID:      "fake-did",
			},
		},
	})
	require.NoError(t, err)

	state, err := db.State()
	require.NoError(t, err)

	require.Contains(t, state.Users, "id1", "user added to state")
	require.Equal(t, "user1", state.Users["id1"].Username, "username matches")

	bytes, err := os.ReadFile(tmpFile)
	require.NoError(t, err)

	stateFromFile, err := FromBytes(bytes)
	require.NoError(t, err)

	require.Contains(t, stateFromFile.Users, "id1", "user added to state from file")
	require.Equal(t, "user1", stateFromFile.Users["id1"].Username, "username matches from file")

	dbBytes, err := db.Bytes()
	require.NoError(t, err)
	require.Equal(t, string(dbBytes), string(bytes), "bytes from db match file")
}
