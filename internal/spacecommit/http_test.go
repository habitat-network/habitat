package spacecommit

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
)

func TestSignedCommitToXRPCAndFromXRPCRoundTrip(t *testing.T) {
	c := SignedCommit{
		Ver:  Version,
		Hash: []byte{0x01, 0x02},
		Ikm:  []byte{0x03, 0x04},
		Mac:  []byte{0x05, 0x06},
		Sig:  []byte{0x07, 0x08},
		Rev:  "3kzl6abcde02k",
	}

	api := c.ToXRPC()
	require.Equal(t, int64(Version), api.Ver)
	require.Equal(t, "3kzl6abcde02k", api.Rev)
	require.Equal(t, atdata.Bytes{0x01, 0x02}, api.Hash)
	require.Equal(t, atdata.Bytes{0x03, 0x04}, api.Ikm)
	require.Equal(t, atdata.Bytes{0x05, 0x06}, api.Mac)
	require.Equal(t, atdata.Bytes{0x07, 0x08}, api.Sig)

	got := FromXRPC(api)
	require.Equal(t, c, got)
}

func TestFromXRPC(t *testing.T) {
	api := habitat.NetworkHabitatSpaceDefsSignedCommit{
		Ver:  int64(Version),
		Hash: atdata.Bytes{0xaa},
		Ikm:  atdata.Bytes{0xbb},
		Mac:  atdata.Bytes{0xcc},
		Sig:  atdata.Bytes{0xdd},
		Rev:  "3kzl6abcde02k",
	}

	got := FromXRPC(api)
	require.Equal(t, SignedCommit{
		Ver:  Version,
		Hash: []byte{0xaa},
		Ikm:  []byte{0xbb},
		Mac:  []byte{0xcc},
		Sig:  []byte{0xdd},
		Rev:  "3kzl6abcde02k",
	}, got)
}
