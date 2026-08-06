package spaces

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"gocloud.dev/blob"
	"gocloud.dev/gcerrors"

	// Registered blob drivers, selected by the bucket connection string scheme:
	// s3:// , gs:// , azblob:// , file:// , mem:// . Credentials are supplied
	// inline in the connection string (see gocloud.dev/blob driver docs).
	_ "gocloud.dev/blob/fileblob"
	_ "gocloud.dev/blob/gcsblob"
	_ "gocloud.dev/blob/memblob"
	_ "gocloud.dev/blob/s3blob"
)

// ErrBlobNotFound is returned when a blob with the requested CID is not present
// in the store.
var ErrBlobNotFound = errors.New("blob not found")

// BlobStore persists blob bytes content-addressed by their CID. Blobs are
// global to the store (keyed only by CID); permission enforcement happens at
// the space layer that references them.
type BlobStore interface {
	// PutBlob stores data under its computed CID, returning the CID and byte
	// size. Storing identical content again is idempotent.
	PutBlob(ctx context.Context, mimeType string, data []byte) (cid.Cid, int64, error)
	// GetBlob returns a blob's stored bytes and mime type by CID, or
	// ErrBlobNotFound if it is not present.
	GetBlob(ctx context.Context, c cid.Cid) (mimeType string, data []byte, err error)
}

// bucketBlobStore is a BlobStore backed by a gocloud.dev blob.Bucket, so the
// backing store (S3, GCS, Azure, local filesystem, in-memory) is chosen by the
// bucket connection string rather than by code.
type bucketBlobStore struct {
	bucket *blob.Bucket
}

// NewBlobStore wraps an opened gocloud blob.Bucket as a BlobStore.
func NewBlobStore(bucket *blob.Bucket) BlobStore {
	return &bucketBlobStore{bucket: bucket}
}

func (b *bucketBlobStore) PutBlob(
	ctx context.Context,
	mimeType string,
	data []byte,
) (cid.Cid, int64, error) {
	// "blessed" blob CID: CIDv1, raw codec, sha-256. https://atproto.com/specs/blob
	c, err := cid.NewPrefixV1(cid.Raw, multihash.SHA2_256).Sum(data)
	if err != nil {
		return cid.Undef, 0, fmt.Errorf("compute blob cid: %w", err)
	}

	w, err := b.bucket.NewWriter(ctx, c.String(), &blob.WriterOptions{ContentType: mimeType})
	if err != nil {
		return cid.Undef, 0, fmt.Errorf("open blob writer: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		_ = w.Close()
		return cid.Undef, 0, fmt.Errorf("write blob: %w", err)
	}
	if err := w.Close(); err != nil {
		return cid.Undef, 0, fmt.Errorf("close blob writer: %w", err)
	}
	return c, int64(len(data)), nil
}

func (b *bucketBlobStore) GetBlob(
	ctx context.Context,
	c cid.Cid,
) (string, []byte, error) {
	r, err := b.bucket.NewReader(ctx, c.String(), nil)
	if err != nil {
		if gcerrors.Code(err) == gcerrors.NotFound {
			return "", nil, ErrBlobNotFound
		}
		return "", nil, fmt.Errorf("open blob reader: %w", err)
	}
	defer func() { _ = r.Close() }()

	data, err := io.ReadAll(r)
	if err != nil {
		return "", nil, fmt.Errorf("read blob: %w", err)
	}
	return r.ContentType(), data, nil
}
