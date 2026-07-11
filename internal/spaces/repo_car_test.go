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
)

func TestSerializeRepoCAR(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "repo-car")
	require.NoError(t, err)

	collA := syntax.NSID("network.habitat.note")
	collB := syntax.NSID("network.habitat.post")
	_, _, err = s.PutRecord(
		t.Context(),
		uri,
		owner,
		collB,
		"b-key",
		map[string]any{"text": "second"},
	)
	require.NoError(t, err)
	_, _, err = s.PutRecord(
		t.Context(),
		uri,
		owner,
		collA,
		"a-key",
		map[string]any{"text": "first"},
	)
	require.NoError(t, err)

	blocks, err := s.ListRecordBlocks(t.Context(), uri, owner)
	require.NoError(t, err)
	require.Len(t, blocks, 2)

	carBytes, err := SerializeRepoCAR(uri, owner, blocks)
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
	require.Equal(t, int64(1), commit["ver"])
	require.Equal(t, "placeholder", string(commit["sig"].(atdata.Bytes)))

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

func TestSerializeRepoCAR_EmptyRepo(t *testing.T) {
	t.Parallel()

	carBytes, err := SerializeRepoCAR("ats://did:plc:org/network.habitat.group/empty", owner, nil)
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
	require.Equal(t, 2, blockCount)
}
