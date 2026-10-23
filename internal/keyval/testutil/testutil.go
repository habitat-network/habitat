// Package testutil builds keyval stores for tests.
package testutil

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/keyval"
)

// New returns a store over its own throwaway database, so tests do not see
// each other's entries. It is not namespaced: the namespace only keeps
// consumers out of each other's rows, and each test has a store to itself.
func New(t *testing.T) keyval.Store {
	t.Helper()
	kv, err := keyval.New(t.Context(), testutil.NewDB(t, keyval.Models()))
	require.NoError(t, err)
	return kv
}

// NewEncrypted returns an encrypted store over its own throwaway database,
// keyed with [encrypt.TestKey]. Like [New], it is not namespaced.
func NewEncrypted(t *testing.T) keyval.EncryptedStore {
	t.Helper()
	es, err := keyval.NewEncrypted(New(t), encrypt.TestKey)
	require.NoError(t, err)
	return es
}
