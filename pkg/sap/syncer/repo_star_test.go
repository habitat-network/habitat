package syncer

import (
	"bytes"
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

func TestDecodeCommit(t *testing.T) {
	t.Parallel()

	hash := []byte{0xaa, 0xbb}
	m := map[string]any{
		"ver":   int64(spacecommit.Version),
		"repo":  "did:plc:alice",
		"space": "at://did:plc:owner/space/network.habitat.space/s1",
		"rev":   "3kzl6abcde02k",
		"data":  atdata.Bytes(hash),
	}
	c, err := decodeCommit(m)
	require.NoError(t, err)
	require.Equal(t, spacecommit.Commit{
		Ver:   spacecommit.Version,
		Repo:  "did:plc:alice",
		Space: "at://did:plc:owner/space/network.habitat.space/s1",
		Rev:   "3kzl6abcde02k",
		Data:  hash,
	}, c)
}

func TestDecodeCommitErrors(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		meta map[string]any
		want string
	}{
		"missing data": {map[string]any{"ver": int64(1), "rev": "3kzl6abcde02k"}, "missing data"},
		"bad ver":      {map[string]any{"ver": "notanumber", "rev": "3kzl6abcde02k"}, "ver is not an integer"},
		"bad rev":      {map[string]any{"ver": int64(1), "rev": int64(42)}, "rev is not a string"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := decodeCommit(tc.meta)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

// starRecord is one record to put in a test archive.
type starRecord struct {
	path  string
	value map[string]any
}

// writeTestStar builds a getRepo archive whose commit names space and repo and
// whose data is the set hash of records, written in the order given.
func writeTestStar(
	t *testing.T,
	meta map[string]any,
	records []starRecord,
) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := star.NewWriter(&buf, meta)
	require.NoError(t, err)
	for _, rec := range records {
		b, err := atdata.MarshalCBOR(rec.value)
		require.NoError(t, err)
		require.NoError(t, w.WritePair([]byte(rec.path), b))
	}
	return buf.Bytes()
}

func testStarMeta(
	t *testing.T,
	space habitat_syntax.SpaceURI,
	repoDID syntax.DID,
	records []starRecord,
) map[string]any {
	t.Helper()
	var lt spacecommit.LtHash
	for _, rec := range records {
		b, err := atdata.MarshalCBOR(rec.value)
		require.NoError(t, err)
		recCID, err := cid.V1Builder{Codec: cid.DagCBOR, MhType: multihash.SHA2_256}.Sum(b)
		require.NoError(t, err)
		coll, rkey, err := splitRecordPath(rec.path)
		require.NoError(t, err)
		lt.Add(spacecommit.RecordElement(coll, rkey, recCID.String()))
	}
	return map[string]any{
		"$type": repoStarType,
		"ver":   int64(spacecommit.Version),
		"repo":  repoDID.String(),
		"space": space.String(),
		"rev":   "3kzl6abcde02k",
		"data":  atdata.Bytes(lt.Sum()),
	}
}

// buildMinimalStar builds a getRepo archive holding one record,
// network.habitat.test/rec1, whose commit hash matches it.
func buildMinimalStar(t *testing.T, space habitat_syntax.SpaceURI, repoDID syntax.DID) []byte {
	t.Helper()
	records := []starRecord{
		{
			path:  "network.habitat.test/rec1",
			value: map[string]any{"$type": "network.habitat.test", "val": "hello"},
		},
	}
	return writeTestStar(t, testStarMeta(t, space, repoDID, records), records)
}

const (
	testSpace = habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
	testRepo  = syntax.DID("did:plc:alice")
)

func TestParseRepoStar(t *testing.T) {
	t.Parallel()

	records := []starRecord{
		{path: "network.habitat.a/long-rkey", value: map[string]any{"n": int64(1)}},
		{path: "network.habitat.b/s", value: map[string]any{"n": int64(2)}},
	}
	meta := testStarMeta(t, testSpace, testRepo, records)
	got, err := parseRepoStar(bytes.NewReader(writeTestStar(t, meta, records)), testSpace, testRepo)
	require.NoError(t, err)
	require.Equal(t, "3kzl6abcde02k", got.Commit.Rev)
	require.Equal(t, []byte(meta["data"].(atdata.Bytes)), got.Commit.Data)
	require.Len(t, got.Records, 2)
	require.Equal(t, syntax.NSID("network.habitat.a"), got.Records[0].Collection)
	require.Equal(t, syntax.RecordKey("long-rkey"), got.Records[0].Rkey)
	require.Equal(t, int64(1), got.Records[0].Value["n"])
	require.Equal(t, syntax.RecordKey("s"), got.Records[1].Rkey)

	// The recovered CIDs fold to the commit's hash.
	var lt spacecommit.LtHash
	for _, rec := range got.Records {
		lt.Add(spacecommit.RecordElement(rec.Collection, rec.Rkey, rec.Cid.String()))
	}
	require.Equal(t, got.Commit.Data, lt.Sum())
}

func TestParseRepoStarErrors(t *testing.T) {
	t.Parallel()

	one := []starRecord{{path: "network.habitat.a/k", value: map[string]any{"n": int64(1)}}}
	validMeta := func() map[string]any { return testStarMeta(t, testSpace, testRepo, one) }
	withMeta := func(key string, value any) []byte {
		m := validMeta()
		m[key] = value
		return writeTestStar(t, m, one)
	}

	minimal := buildMinimalStar(t, testSpace, testRepo)
	truncated := minimal[:len(minimal)-1]

	tests := []struct {
		name    string
		archive []byte
		want    string
	}{
		{name: "not star", archive: []byte("nope"), want: "read star header"},
		{
			name:    "wrong profile",
			archive: withMeta("$type", "atproto-space-meta"),
			want:    "unexpected star profile",
		},
		{name: "wrong repo", archive: withMeta("repo", "did:plc:bob"), want: "commit is for repo"},
		{
			name:    "wrong space",
			archive: withMeta("space", "at://did:plc:owner/space/network.habitat.space/s2"),
			want:    "commit is for space",
		},
		{name: "bad commit", archive: withMeta("rev", int64(1)), want: "rev is not a string"},
		{
			name: "out of order",
			archive: writeTestStar(t, validMeta(), []starRecord{
				{path: "network.habitat.b/k", value: map[string]any{}},
				{path: "network.habitat.a/k", value: map[string]any{}},
			}),
			want: "out of order",
		},
		{
			name: "duplicate path",
			archive: writeTestStar(t, validMeta(), []starRecord{
				{path: "network.habitat.a/k", value: map[string]any{}},
				{path: "network.habitat.a/k", value: map[string]any{}},
			}),
			want: "out of order",
		},
		{
			name:    "bad path",
			archive: writeTestStar(t, validMeta(), []starRecord{{path: "noslash", value: map[string]any{}}}),
			want:    "invalid record path",
		},
		{name: "truncated", archive: truncated, want: "read star pair"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseRepoStar(bytes.NewReader(tt.archive), testSpace, testRepo)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestParseRepoStarBadRecordCBOR(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w, err := star.NewWriter(&buf, testStarMeta(t, testSpace, testRepo, nil))
	require.NoError(t, err)
	require.NoError(t, w.WritePair([]byte("network.habitat.a/k"), []byte{0xff}))
	_, err = parseRepoStar(&buf, testSpace, testRepo)
	require.ErrorContains(t, err, "decode record")
}
