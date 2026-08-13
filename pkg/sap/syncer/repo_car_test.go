package syncer

import (
	"bytes"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	car "github.com/ipld/go-car"
	util "github.com/ipld/go-car/util"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/spacecommit"
)

func TestSplitRecordPath(t *testing.T) {
	t.Parallel()

	coll, rkey, err := splitRecordPath("network.habitat.test/abc123")
	require.NoError(t, err)
	require.Equal(t, syntax.NSID("network.habitat.test"), coll)
	require.Equal(t, syntax.RecordKey("abc123"), rkey)
}

func TestSplitRecordPathNoSlash(t *testing.T) {
	t.Parallel()

	_, _, err := splitRecordPath("noslash")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid record path")
}

func TestSplitRecordPathInvalidNSID(t *testing.T) {
	t.Parallel()

	_, _, err := splitRecordPath("not a nsi d/abc")
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse collection")
}

func TestSplitRecordPathInvalidRkey(t *testing.T) {
	t.Parallel()

	_, _, err := splitRecordPath("network.habitat.test/!!!")
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse rkey")
}

func TestCommitBytesFieldNil(t *testing.T) {
	t.Parallel()

	b, err := commitBytesField(map[string]any{"x": nil}, "x")
	require.NoError(t, err)
	require.Nil(t, b)
}

func TestCommitBytesFieldMissing(t *testing.T) {
	t.Parallel()

	b, err := commitBytesField(map[string]any{}, "missing")
	require.NoError(t, err)
	require.Nil(t, b)
}

func TestCommitBytesFieldBytes(t *testing.T) {
	t.Parallel()

	b, err := commitBytesField(map[string]any{"x": atdata.Bytes([]byte{1, 2, 3})}, "x")
	require.NoError(t, err)
	require.Equal(t, []byte{1, 2, 3}, b)
}

func TestCommitBytesFieldBadType(t *testing.T) {
	t.Parallel()

	_, err := commitBytesField(map[string]any{"x": int64(42)}, "x")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a bytes field")
}

func TestDecodeSignedCommit(t *testing.T) {
	t.Parallel()

	hash := []byte{0xaa, 0xbb}
	m := map[string]any{
		"ver":  int64(spacecommit.Version),
		"rev":  "3kzl6abcde02k",
		"hash": atdata.Bytes(hash),
	}
	b, err := atdata.MarshalCBOR(m)
	require.NoError(t, err)

	c, err := decodeSignedCommit(b)
	require.NoError(t, err)
	require.Equal(t, spacecommit.Version, c.Ver)
	require.Equal(t, "3kzl6abcde02k", c.Rev)
	require.Equal(t, hash, c.Hash)
	require.Nil(t, c.Ikm)
	require.Nil(t, c.Mac)
	require.Nil(t, c.Sig)
}

func TestDecodeSignedCommitMissingHash(t *testing.T) {
	t.Parallel()

	m := map[string]any{
		"ver": int64(spacecommit.Version),
		"rev": "3kzl6abcde02k",
	}
	b, err := atdata.MarshalCBOR(m)
	require.NoError(t, err)

	_, err = decodeSignedCommit(b)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing hash")
}

func TestDecodeSignedCommitBadVer(t *testing.T) {
	t.Parallel()

	m := map[string]any{
		"ver": "notanumber",
		"rev": "3kzl6abcde02k",
	}
	b, err := atdata.MarshalCBOR(m)
	require.NoError(t, err)

	_, err = decodeSignedCommit(b)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ver is not an integer")
}

func TestDecodeSignedCommitBadRev(t *testing.T) {
	t.Parallel()

	m := map[string]any{
		"ver": int64(spacecommit.Version),
		"rev": int64(42),
	}
	b, err := atdata.MarshalCBOR(m)
	require.NoError(t, err)

	_, err = decodeSignedCommit(b)
	require.Error(t, err)
	require.Contains(t, err.Error(), "rev is not a string")
}

func TestDecodeSignedCommitWithOptionalFields(t *testing.T) {
	t.Parallel()

	m := map[string]any{
		"ver":  int64(spacecommit.Version),
		"rev":  "3kzl6abcde02k",
		"hash": atdata.Bytes([]byte{0xaa}),
		"ikm":  atdata.Bytes([]byte{0x01}),
		"mac":  atdata.Bytes([]byte{0x02}),
		"sig":  atdata.Bytes([]byte{0x03}),
	}
	b, err := atdata.MarshalCBOR(m)
	require.NoError(t, err)

	c, err := decodeSignedCommit(b)
	require.NoError(t, err)
	require.Equal(t, []byte{0x01}, c.Ikm)
	require.Equal(t, []byte{0x02}, c.Mac)
	require.Equal(t, []byte{0x03}, c.Sig)
}

func buildMinimalCAR(t *testing.T) []byte {
	t.Helper()

	coll := syntax.NSID("network.habitat.test")
	rkey := syntax.RecordKey("rec1")
	recMap := map[string]any{"$type": "network.habitat.test", "val": "hello"}
	recBytes, err := atdata.MarshalCBOR(recMap)
	require.NoError(t, err)
	recCID, err := cid.V1Builder{}.WithCodec(cid.DagCBOR).Sum(recBytes)
	require.NoError(t, err)

	var lt spacecommit.LtHash
	lt.Add(spacecommit.RecordElement(coll, rkey, recCID.String()))
	commitHash := lt.Sum()

	commitMap := map[string]any{
		"ver":  int64(spacecommit.Version),
		"hash": atdata.Bytes(commitHash),
		"rev":  "3kzl6abcde02k",
	}
	commitBytes, err := atdata.MarshalCBOR(commitMap)
	require.NoError(t, err)
	commitCID, err := cid.V1Builder{}.WithCodec(cid.DagCBOR).Sum(commitBytes)
	require.NoError(t, err)

	indexMap := map[string]any{
		coll.String() + "/" + rkey.String(): atdata.CIDLink(recCID),
	}
	indexBytes, err := atdata.MarshalCBOR(indexMap)
	require.NoError(t, err)
	indexCID, err := cid.V1Builder{}.WithCodec(cid.DagCBOR).Sum(indexBytes)
	require.NoError(t, err)

	var buf bytes.Buffer
	hdr := car.CarHeader{Version: 1, Roots: []cid.Cid{commitCID, indexCID}}
	require.NoError(t, car.WriteHeader(&hdr, &buf))
	require.NoError(t, util.LdWrite(&buf, commitCID.Bytes(), commitBytes))
	require.NoError(t, util.LdWrite(&buf, indexCID.Bytes(), indexBytes))
	require.NoError(t, util.LdWrite(&buf, recCID.Bytes(), recBytes))

	return buf.Bytes()
}
