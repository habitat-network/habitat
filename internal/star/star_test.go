package star

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/stretchr/testify/require"
)

func writeArchive(t *testing.T, meta map[string]any, pairs [][2][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := NewWriter(&buf, meta)
	require.NoError(t, err)
	for _, p := range pairs {
		require.NoError(t, w.WritePair(p[0], p[1]))
	}
	return buf.Bytes()
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	meta := map[string]any{"$type": "test-profile", "ver": int64(1), "data": atdata.Bytes{1, 2}}
	big := bytes.Repeat([]byte{7}, 300) // needs a two-byte varint
	archive := writeArchive(t, meta, [][2][]byte{
		{[]byte("a"), []byte("one")},
		{[]byte("b"), big},
	})
	require.Equal(t, Magic, archive[:4])

	r, err := NewReader(bytes.NewReader(archive), 0)
	require.NoError(t, err)
	require.Equal(t, "test-profile", r.Meta["$type"])
	require.Equal(t, int64(1), r.Meta["ver"])
	require.Equal(t, atdata.Bytes{1, 2}, r.Meta["data"])

	k, v, err := r.Next()
	require.NoError(t, err)
	require.Equal(t, "a", string(k))
	require.Equal(t, "one", string(v))
	k, v, err = r.Next()
	require.NoError(t, err)
	require.Equal(t, "b", string(k))
	require.Equal(t, big, v)
	_, _, err = r.Next()
	require.ErrorIs(t, err, io.EOF)
}

func TestNewWriter_RequiresType(t *testing.T) {
	t.Parallel()
	_, err := NewWriter(io.Discard, map[string]any{"ver": int64(1)})
	require.Error(t, err)
}

func TestNewWriter_PropagatesWriteErrors(t *testing.T) {
	t.Parallel()
	_, err := NewWriter(failWriter{}, map[string]any{"$type": "x"})
	require.Error(t, err)
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("boom") }

func TestReader_Malformed(t *testing.T) {
	t.Parallel()

	valid := writeArchive(t, map[string]any{"$type": "x"}, [][2][]byte{{[]byte("k"), []byte("v")}})
	header := writeArchive(t, map[string]any{"$type": "x"}, nil)
	noType, err := atdata.MarshalCBOR(map[string]any{"ver": int64(1)})
	require.NoError(t, err)
	// cat concatenates byte slices into a fresh one.
	cat := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

	tests := []struct {
		name    string
		archive []byte
		atNext  bool
	}{
		{name: "bad magic", archive: append([]byte{0, 0, 0, 0}, valid[4:]...)},
		{name: "empty", archive: nil},
		{name: "missing metadata", archive: Magic},
		{name: "metadata without type", archive: cat(Magic, []byte{byte(len(noType))}, noType)},
		{name: "metadata not cbor", archive: append(append([]byte{}, Magic...), 1, 0xff)},
		{name: "truncated metadata", archive: valid[:6]},
		{name: "missing value", archive: append(append([]byte{}, header...), 1, 'k'), atNext: true},
		{name: "truncated value", archive: valid[:len(valid)-1], atNext: true},
		{name: "non-canonical varint", archive: cat(header, []byte{0x81, 0x00}), atNext: true},
		{
			name:    "varint too long",
			archive: cat(header, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01}),
			atNext:  true,
		},
		{name: "truncated varint", archive: cat(header, []byte{0x81}), atNext: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, err := NewReader(bytes.NewReader(tt.archive), 0)
			if !tt.atNext {
				require.ErrorIs(t, err, ErrParse)
				return
			}
			require.NoError(t, err)
			_, _, err = r.Next()
			require.ErrorIs(t, err, ErrParse)
		})
	}
}

func TestReader_ValueLimit(t *testing.T) {
	t.Parallel()
	archive := writeArchive(
		t,
		map[string]any{"$type": "x"},
		[][2][]byte{{[]byte("k"), []byte("toolong")}},
	)
	r, err := NewReader(bytes.NewReader(archive), 3)
	require.NoError(t, err)
	_, _, err = r.Next()
	require.ErrorIs(t, err, ErrParse)
}

func TestReader_NonCanonicalMetadata(t *testing.T) {
	t.Parallel()
	// {"b": 1, "a": 2}: keys out of canonical order.
	meta := []byte{0xa3, 0x61, 'b', 0x01, 0x61, 'a', 0x02, 0x65, '$', 't', 'y', 'p', 'e', 0x61, 'x'}
	archive := append(append(append([]byte{}, Magic...), byte(len(meta))), meta...)
	_, err := NewReader(bytes.NewReader(archive), 0)
	require.ErrorIs(t, err, ErrParse)
}
