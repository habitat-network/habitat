package spaces

import (
	"bytes"
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLtHash_EmptyStateIsZeroDigest(t *testing.T) {
	var h ltHash
	require.Equal(t, make([]byte, ltHashStateBytes), h.state())
	want := sha256.Sum256(make([]byte, ltHashStateBytes))
	require.Equal(t, want[:], h.sum())
}

func TestLtHash_AddThenRemoveRestoresEmpty(t *testing.T) {
	var h ltHash
	empty := h.sum()

	h.add(recordElement("network.habitat.note", "abc", "bafyreiaaa"))
	require.NotEqual(t, empty, h.sum(), "adding an element must change the digest")

	h.remove(recordElement("network.habitat.note", "abc", "bafyreiaaa"))
	require.Equal(t, empty, h.sum(), "removing the same element must restore the empty digest")
}

func TestLtHash_OrderIndependent(t *testing.T) {
	a := recordElement("c", "k1", "cid1")
	b := recordElement("c", "k2", "cid2")
	c := recordElement("c", "k3", "cid3")

	var h1 ltHash
	h1.add(a)
	h1.add(b)
	h1.add(c)

	var h2 ltHash
	h2.add(c)
	h2.add(a)
	h2.add(b)

	require.True(t, bytes.Equal(h1.sum(), h2.sum()), "digest must be independent of insertion order")
}

func TestLtHash_DistinctSetsDiffer(t *testing.T) {
	var h1 ltHash
	h1.add(recordElement("c", "k1", "cid1"))

	var h2 ltHash
	h2.add(recordElement("c", "k1", "cid2")) // different cid

	require.False(t, bytes.Equal(h1.sum(), h2.sum()))
}

func TestLtHash_SumIs32Bytes(t *testing.T) {
	var h ltHash
	h.add(recordElement("c", "k", "cid"))
	require.Len(t, h.sum(), 32)
}
