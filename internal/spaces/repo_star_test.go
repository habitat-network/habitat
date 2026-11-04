package spaces

import (
	"bytes"
	"io"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/spacecommit"
	"github.com/habitat-network/habitat/internal/star"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	testStarSpace = habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.test/main")
	testStarRepo  = syntax.DID("did:plc:author")
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
	c, err := cid.NewPrefixV1(cid.DagCBOR, multihash.SHA2_256).Sum(raw)
	require.NoError(t, err)
	return recordBlock{Collection: coll, Rkey: rkey, Cid: c, Bytes: raw}
}

func testCommit() spacecommit.SignedCommit {
	return spacecommit.SignedCommit{
		Ver:           spacecommit.Version,
		Hash:          make([]byte, 32),
		Ikm:           make([]byte, 32),
		Mac:           make([]byte, 32),
		Sig:           []byte("signature"),
		Rev:           "3kabcdefghij2",
		HabitatSigned: true,
	}
}

// readStarPairs reads an archive back, returning its metadata and its pairs as
// path → value in archive order.
func readStarPairs(t *testing.T, archive []byte) (map[string]any, []string, [][]byte) {
	t.Helper()
	r, err := star.NewReader(bytes.NewReader(archive), 0)
	require.NoError(t, err)
	var keys []string
	var values [][]byte
	for {
		k, v, err := r.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		keys = append(keys, string(k))
		values = append(values, v)
	}
	return r.Meta, keys, values
}

func TestSerializeRepoStar(t *testing.T) {
	t.Parallel()

	collA := syntax.NSID("network.habitat.note")
	collB := syntax.NSID("network.habitat.post")
	// Pass the blocks out of order to exercise the sort.
	a := testRecordBlock(t, collA, "a-key", map[string]any{"text": "first"})
	b := testRecordBlock(t, collB, "b-key", map[string]any{"text": "second"})

	archive, err := SerializeRepoStar(
		testStarSpace,
		testStarRepo,
		testCommit(),
		[]recordBlock{b, a},
	)
	require.NoError(t, err)

	meta, keys, values := readStarPairs(t, archive)
	require.Equal(t, RepoStarType, meta["$type"])
	require.Equal(t, int64(spacecommit.Version), meta["ver"])
	require.Equal(t, testStarRepo.String(), meta["repo"])
	require.Equal(t, testStarSpace.String(), meta["space"])
	require.Equal(t, "3kabcdefghij2", meta["rev"])
	require.Equal(t, atdata.Bytes(make([]byte, 32)), meta["data"])
	require.Equal(t, atdata.Bytes("signature"), meta["sig"])
	require.Equal(t, true, meta["habitatSigned"])

	require.Equal(t, []string{"network.habitat.note/a-key", "network.habitat.post/b-key"}, keys)
	require.Equal(t, [][]byte{a.Bytes, b.Bytes}, values)
}

func TestSerializeRepoStar_NoRecords(t *testing.T) {
	t.Parallel()

	archive, err := SerializeRepoStar(testStarSpace, testStarRepo, testCommit(), nil)
	require.NoError(t, err)
	meta, keys, _ := readStarPairs(t, archive)
	require.Equal(t, RepoStarType, meta["$type"])
	require.Empty(t, keys)
}

// TestSerializeRepoStar_BytewiseOrder verifies pairs are in plain bytewise
// order of the path, not the length-first order of a DAG-CBOR map.
func TestSerializeRepoStar_BytewiseOrder(t *testing.T) {
	t.Parallel()

	long := testRecordBlock(t, "network.habitat.a", "long-rkey", map[string]any{"n": 1})
	short := testRecordBlock(t, "network.habitat.b", "s", map[string]any{"n": 2})
	archive, err := SerializeRepoStar(
		testStarSpace,
		testStarRepo,
		testCommit(),
		[]recordBlock{short, long},
	)
	require.NoError(t, err)

	_, keys, _ := readStarPairs(t, archive)
	require.Equal(t, []string{"network.habitat.a/long-rkey", "network.habitat.b/s"}, keys)
}

func TestSerializeRepoStar_DuplicatePath(t *testing.T) {
	t.Parallel()

	rec := testRecordBlock(t, "network.habitat.a", "k", map[string]any{"n": 1})
	_, err := SerializeRepoStar(testStarSpace, testStarRepo, testCommit(), []recordBlock{rec, rec})
	require.ErrorContains(t, err, "duplicate record path")
}

// TestSerializeRepoStar_HashOnlyCommit verifies a commit without ikm, sig or
// mac still yields a readable archive: the empty fields are omitted.
func TestSerializeRepoStar_HashOnlyCommit(t *testing.T) {
	t.Parallel()

	commit := spacecommit.SignedCommit{
		Ver:  spacecommit.Version,
		Hash: make([]byte, 32),
		Rev:  "3kabcdefghij2",
	}
	archive, err := SerializeRepoStar(testStarSpace, testStarRepo, commit, nil)
	require.NoError(t, err)
	meta, _, _ := readStarPairs(t, archive)
	require.NotContains(t, meta, "ikm")
	require.NotContains(t, meta, "sig")
	require.NotContains(t, meta, "mac")
}
