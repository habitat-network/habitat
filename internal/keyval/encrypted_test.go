package keyval_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/keyval"
	"github.com/habitat-network/habitat/internal/keyval/testutil"
)

type secret struct {
	Token string
	N     int
}

func TestEncryptedStoreRoundTrip(t *testing.T) {
	kv := testutil.New(t)
	es, err := keyval.NewEncrypted(kv, encrypt.TestKey)
	require.NoError(t, err)
	want := secret{Token: "hunter2", N: 7}

	require.NoError(t, es.PutEncrypted(t.Context(), "k", want, time.Minute))

	var got secret
	require.NoError(t, es.GetEncrypted(t.Context(), "k", &got))
	require.Equal(t, want, got)

	// The backing store holds ciphertext, not the plaintext value.
	var raw string
	require.NoError(t, kv.Get(t.Context(), "k", &raw))
	require.NotContains(t, raw, "hunter2")

	var consumed secret
	require.NoError(t, es.ConsumeEncrypted(t.Context(), "k", &consumed))
	require.Equal(t, want, consumed)
	require.ErrorIs(t, es.GetEncrypted(t.Context(), "k", &got), keyval.ErrNotFound)
}

func TestEncryptedStoreNamespace(t *testing.T) {
	kv := testutil.New(t)
	es, err := keyval.NewEncrypted(kv, encrypt.TestKey)
	require.NoError(t, err)
	a, b := es.Namespace("a"), es.Namespace("b")

	require.NoError(t, a.PutEncrypted(t.Context(), "k", secret{N: 1}, time.Minute))

	var got secret
	require.NoError(t, a.GetEncrypted(t.Context(), "k", &got))
	require.Equal(t, secret{N: 1}, got)
	require.ErrorIs(t, b.GetEncrypted(t.Context(), "k", &got), keyval.ErrNotFound)
	// The child writes into the backing store's namespace.
	var raw string
	require.NoError(t, kv.Namespace("a").Get(t.Context(), "k", &raw))
}

func TestEncryptedStorePutIfAbsent(t *testing.T) {
	es := testutil.NewEncrypted(t)

	require.NoError(t, es.PutIfAbsentEncrypted(t.Context(), "k", secret{N: 1}, time.Minute))
	require.ErrorIs(
		t,
		es.PutIfAbsentEncrypted(t.Context(), "k", secret{N: 2}, time.Minute),
		keyval.ErrAlreadyExists,
	)
}

func TestEncryptedStoreWrongKey(t *testing.T) {
	kv := testutil.New(t)
	es, err := keyval.NewEncrypted(kv, encrypt.TestKey)
	require.NoError(t, err)
	require.NoError(t, es.PutEncrypted(t.Context(), "k", secret{N: 1}, time.Minute))

	other, err := keyval.NewEncrypted(kv, []byte("another-encryption-key32-0123456"))
	require.NoError(t, err)
	var got secret
	require.Error(t, other.GetEncrypted(t.Context(), "k", &got))
}

func TestNewEncryptedRejectsBadKey(t *testing.T) {
	_, err := keyval.NewEncrypted(testutil.New(t), []byte("short"))
	require.Error(t, err)
}

func TestStoreNoExpiry(t *testing.T) {
	kv := testutil.New(t)
	es, err := keyval.NewEncrypted(kv, encrypt.TestKey)
	require.NoError(t, err)

	require.NoError(t, kv.Put(t.Context(), "plain", secret{N: 1}, keyval.NoExpiry))
	require.NoError(t, es.PutEncrypted(t.Context(), "enc", secret{N: 2}, keyval.NoExpiry))

	var got secret
	require.NoError(t, kv.Get(t.Context(), "plain", &got))
	require.Equal(t, secret{N: 1}, got)
	require.NoError(t, es.GetEncrypted(t.Context(), "enc", &got))
	require.Equal(t, secret{N: 2}, got)
}
