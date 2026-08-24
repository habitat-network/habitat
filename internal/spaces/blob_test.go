package spaces

import (
	"context"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"
	"gocloud.dev/blob/memblob"
)

// setupBlobStore returns a BlobStore backed by an in-memory bucket, and a
// teardown function that closes it.
func setupBlobStore(t *testing.T) (BlobStore, func()) {
	t.Helper()
	bucket := memblob.OpenBucket(nil)
	return NewBlobStore(bucket), func() {
		_ = bucket.Close()
	}
}

func TestBlobStorePutGet(t *testing.T) {
	t.Parallel()

	store, teardown := setupBlobStore(t)
	defer teardown()

	ctx := context.Background()
	data := []byte("hello blob")

	c, size, err := store.PutBlob(ctx, "text/plain", data)
	require.NoError(t, err)
	require.Equal(t, int64(len(data)), size)

	wantCID, err := cid.NewPrefixV1(cid.Raw, multihash.SHA2_256).Sum(data)
	require.NoError(t, err)
	require.Equal(t, wantCID, c)

	gotMime, gotData, err := store.GetBlob(ctx, c)
	require.NoError(t, err)
	require.Equal(t, "text/plain", gotMime)
	require.Equal(t, data, gotData)
}

func TestBlobStorePutIdempotent(t *testing.T) {
	t.Parallel()

	store, teardown := setupBlobStore(t)
	defer teardown()

	ctx := context.Background()
	data := []byte("duplicate content")

	c1, size1, err := store.PutBlob(ctx, "text/plain", data)
	require.NoError(t, err)
	c2, size2, err := store.PutBlob(ctx, "text/plain", data)
	require.NoError(t, err)

	require.Equal(t, c1, c2)
	require.Equal(t, size1, size2)

	_, gotData, err := store.GetBlob(ctx, c1)
	require.NoError(t, err)
	require.Equal(t, data, gotData)
}

func TestBlobStoreGetNotFound(t *testing.T) {
	t.Parallel()

	store, teardown := setupBlobStore(t)
	defer teardown()

	ctx := context.Background()
	missingCID, err := cid.NewPrefixV1(cid.Raw, multihash.SHA2_256).Sum([]byte("never stored"))
	require.NoError(t, err)

	_, _, err = store.GetBlob(ctx, missingCID)
	require.ErrorIs(t, err, ErrBlobNotFound)
}

func TestBlobStoreEmptyData(t *testing.T) {
	t.Parallel()

	store, teardown := setupBlobStore(t)
	defer teardown()

	ctx := context.Background()

	c, size, err := store.PutBlob(ctx, "application/octet-stream", []byte{})
	require.NoError(t, err)
	require.Equal(t, int64(0), size)

	gotMime, gotData, err := store.GetBlob(ctx, c)
	require.NoError(t, err)
	require.Equal(t, "application/octet-stream", gotMime)
	require.Empty(t, gotData)
}

func TestBlobStoreDistinctContentDistinctCIDs(t *testing.T) {
	t.Parallel()

	store, teardown := setupBlobStore(t)
	defer teardown()

	ctx := context.Background()

	c1, _, err := store.PutBlob(ctx, "text/plain", []byte("content a"))
	require.NoError(t, err)
	c2, _, err := store.PutBlob(ctx, "text/plain", []byte("content b"))
	require.NoError(t, err)

	require.NotEqual(t, c1, c2)
}
