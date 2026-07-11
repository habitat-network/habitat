package spacecommit

import (
	"bytes"
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLtHash_EmptyStateIsZeroDigest(t *testing.T) {
	var h LtHash
	require.Equal(t, make([]byte, LtHashStateBytes), h.State())
	want := sha256.Sum256(make([]byte, LtHashStateBytes))
	require.Equal(t, want[:], h.Sum())
}

func TestLtHash_AddThenRemoveRestoresEmpty(t *testing.T) {
	var h LtHash
	empty := h.Sum()

	h.Add(RecordElement("network.habitat.note", "abc", "bafyreiaaa"))
	require.NotEqual(t, empty, h.Sum(), "adding an element must change the digest")

	h.Remove(RecordElement("network.habitat.note", "abc", "bafyreiaaa"))
	require.Equal(t, empty, h.Sum(), "removing the same element must restore the empty digest")
}

func TestLtHash_OrderIndependent(t *testing.T) {
	a := RecordElement("c", "k1", "cid1")
	b := RecordElement("c", "k2", "cid2")
	c := RecordElement("c", "k3", "cid3")

	var h1 LtHash
	h1.Add(a)
	h1.Add(b)
	h1.Add(c)

	var h2 LtHash
	h2.Add(c)
	h2.Add(a)
	h2.Add(b)

	require.True(
		t,
		bytes.Equal(h1.Sum(), h2.Sum()),
		"digest must be independent of insertion order",
	)
}

func TestLtHash_DistinctSetsDiffer(t *testing.T) {
	var h1 LtHash
	h1.Add(RecordElement("c", "k1", "cid1"))

	var h2 LtHash
	h2.Add(RecordElement("c", "k1", "cid2")) // different cid

	require.False(t, bytes.Equal(h1.Sum(), h2.Sum()))
}

func TestLtHash_LoadRoundTrip(t *testing.T) {
	var h LtHash
	h.Add(RecordElement("c", "k1", "cid1"))
	h.Add(RecordElement("c", "k2", "cid2"))

	reloaded := Load(h.State())
	require.Equal(t, h.Sum(), reloaded.Sum())
}

func TestLtHash_SumIs32Bytes(t *testing.T) {
	var h LtHash
	h.Add(RecordElement("c", "k", "cid"))
	require.Len(t, h.Sum(), 32)
}
