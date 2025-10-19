package privi

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSQLiteRepoPutAndGetRecord(t *testing.T) {
	testDBPath := filepath.Join(os.TempDir(), "test_privi.db")
	defer func() { require.NoError(t, os.Remove(testDBPath)) }()

	priviDB, err := sql.Open("sqlite3", testDBPath)
	require.NoError(t, err)

	repo, err := NewSQLiteRepo(priviDB)
	require.NoError(t, err)

	key := "test-key"
	val := map[string]any{"data": "value", "data-1": float64(123), "data-2": true}

	err = repo.putRecord("my-did", key, val, nil)
	require.NoError(t, err)

	got, err := repo.getRecord("my-did", key)
	require.NoError(t, err)

	for k, v := range val {
		_, ok := got[k]
		require.True(t, ok)
		require.Equal(t, got[k], v)
	}
}
