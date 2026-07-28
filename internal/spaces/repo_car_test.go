package spaces

import (
	"bytes"
	"io"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	"github.com/ipld/go-car"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/spacecommit"
)

func testRecordBlock(
	t *testing.T,
	coll syntax.NSID,
	rkey syntax.RecordKey,
	value map[string]any,
) recordBlock {
	t.Helper()
	raw, err := atdata.MarshalCBOR(value)
	require.NoError(t, err)
	c, err := cid.V1Builder{}.WithCodec(cid.DagCBOR).Sum(raw)
	require.NoError(t, err)
	return recordBlock{Collection: coll, Rkey: rkey, Cid: c, Bytes: raw}
}

func testCommit() spacecommit.SignedCommit {
	return spacecommit.SignedCommit{
		Ver:  spacecommit.Version,
		Hash: make([]byte, 32),
		Ikm:  make([]byte, 32),
		Mac:  make([]byte, 32),
		Sig:  []byte("signature"),
		Rev:  "3kabcdefghij2",
	}
}

func TestSerializeRepoCAR(t *testing.T) {
	t.Parallel()

	collA := syntax.NSID("network.habitat.note")
	collB := syntax.NSID("network.habitat.post")
	// Pass the blocks out of lexicographic order to exercise the sort.
	blocks := []recordBlock{
		testRecordBlock(t, collB, "b-key", map[string]any{"text": "second"}),
		testRecordBlock(t, collA, "a-key", map[string]any{"text": "first"}),
	}

	carBytes, err := SerializeRepoCAR(testCommit(), blocks)
	require.NoError(t, err)

	reader, err := car.NewCarReader(bytes.NewReader(carBytes))
	require.NoError(t, err)
	require.Equal(t, uint64(1), reader.Header.Version)
	require.Len(t, reader.Header.Roots, 2)

	commitCID := reader.Header.Roots[0]
	indexCID := reader.Header.Roots[1]

	var commitBlock, indexBlock []byte
	var recordBlocks [][]byte
	for {
		blk, err := reader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)

		switch blk.Cid() {
		case commitCID:
			commitBlock = blk.RawData()
		case indexCID:
			indexBlock = blk.RawData()
		default:
			recordBlocks = append(recordBlocks, blk.RawData())
		}
	}

	require.NotEmpty(t, commitBlock)
	require.NotEmpty(t, indexBlock)
	require.Len(t, recordBlocks, 2)

	commit, err := atdata.UnmarshalCBOR(commitBlock)
	require.NoError(t, err)
	require.Equal(t, int64(spacecommit.Version), commit["ver"])
	require.Equal(t, "3kabcdefghij2", commit["rev"])
	sig, ok := commit["sig"].(atdata.Bytes)
	require.True(t, ok)
	require.Equal(t, "signature", string(sig))

	index, err := atdata.UnmarshalCBOR(indexBlock)
	require.NoError(t, err)
	require.Len(t, index, 2)

	pathA := recordPath(collA, "a-key")
	pathB := recordPath(collB, "b-key")
	linkA, ok := index[pathA].(atdata.CIDLink)
	require.True(t, ok)
	linkB, ok := index[pathB].(atdata.CIDLink)
	require.True(t, ok)

	recordCIDs := make(map[cid.Cid][]byte, len(recordBlocks))
	for _, raw := range recordBlocks {
		recordCID, err := cid.V1Builder{}.WithCodec(cid.DagCBOR).Sum(raw)
		require.NoError(t, err)
		recordCIDs[recordCID] = raw
	}
	require.Contains(t, recordCIDs, cid.Cid(linkA))
	require.Contains(t, recordCIDs, cid.Cid(linkB))
}

func TestSerializeRepoCAR_NoRecords(t *testing.T) {
	t.Parallel()

	carBytes, err := SerializeRepoCAR(testCommit(), nil)
	require.NoError(t, err)

	reader, err := car.NewCarReader(bytes.NewReader(carBytes))
	require.NoError(t, err)
	require.Len(t, reader.Header.Roots, 2)

	blockCount := 0
	for {
		_, err := reader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		blockCount++
	}
	// Only the signed commit and index roots; no record blocks.
	require.Equal(t, 2, blockCount)
}
