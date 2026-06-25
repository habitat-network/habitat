package syntax

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSpaceKey(t *testing.T) {
	generated := NewSkey("testKey")
	require.NotEmpty(t, generated.String())

	parsed, err := ParseSkey("my-space.1")
	require.NoError(t, err)
	require.Equal(t, SpaceKey("my-space.1"), parsed)

	_, err = ParseSkey("")
	require.Error(t, err)
}

func TestConstructSpaceURI(t *testing.T) {
	uri := ConstructSpaceURI("did:plc:abc123", "network.habitat.space", "my-space")
	require.Equal(t, SpaceURI("ats://did:plc:abc123/network.habitat.space/my-space"), uri)
	require.Equal(t, "ats://did:plc:abc123/network.habitat.space/my-space", uri.String())
	require.Equal(t, "did:plc:abc123", uri.SpaceOwner().String())
	require.Equal(t, "network.habitat.space", uri.SpaceType().String())
	require.Equal(t, SpaceKey("my-space"), uri.Skey())
}

func TestParseSpaceURI(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		uri, err := ParseSpaceURI("ats://did:plc:abc123/network.habitat.space/my-space_1")
		require.NoError(t, err)
		require.Equal(t, "did:plc:abc123", uri.SpaceOwner().String())
		require.Equal(t, "network.habitat.space", uri.SpaceType().String())
		require.Equal(t, SpaceKey("my-space_1"), uri.Skey())
	})

	t.Run("too long", func(t *testing.T) {
		_, err := ParseSpaceURI(
			"ats://did:plc:abc123/network.habitat.space/" + strings.Repeat("a", 8193),
		)
		require.Error(t, err)
	})

	t.Run("invalid format", func(t *testing.T) {
		_, err := ParseSpaceURI("habitat://did:plc:abc123/network.habitat.space/my-space")
		require.Error(t, err)
	})

	t.Run("invalid DID", func(t *testing.T) {
		_, err := ParseSpaceURI("ats://not-a-did/network.habitat.space/my-space")
		require.Error(t, err)
	})

	t.Run("invalid type", func(t *testing.T) {
		_, err := ParseSpaceURI("ats://did:plc:abc123/not_a_nsid/my-space")
		require.Error(t, err)
	})
}

func TestSpaceURIAccessorsReturnEmptyForInvalidURI(t *testing.T) {
	uri := SpaceURI("not-a-space-uri")
	require.Empty(t, uri.SpaceOwner())
	require.Empty(t, uri.SpaceType())
	require.Empty(t, uri.Skey())

	uri = SpaceURI("ats://not-a-did/network.habitat.space/my-space")
	require.Empty(t, uri.SpaceOwner())
	require.Equal(t, "network.habitat.space", uri.SpaceType().String())
	require.Equal(t, SpaceKey("my-space"), uri.Skey())

	uri = SpaceURI("ats://did:plc:abc123/not_a_nsid/my-space")
	require.Empty(t, uri.SpaceType())
}

func TestConstructSpaceRecordURI(t *testing.T) {
	uri := ConstructSpaceRecordURI(
		"ats://did:plc:abc123/network.habitat.space/my-space",
		"did:plc:repo456",
		"network.habitat.note",
		"rkey789",
	)
	require.Equal(
		t,
		SpaceRecordURI(
			"ats://did:plc:abc123/network.habitat.space/my-space/did:plc:repo456/network.habitat.note/rkey789",
		),
		uri,
	)
	require.Equal(
		t,
		"ats://did:plc:abc123/network.habitat.space/my-space/did:plc:repo456/network.habitat.note/rkey789",
		uri.String(),
	)
	require.Equal(t, "network.habitat.note", uri.Collection().String())
}

func TestSpaceRecordURI_RepoAndRkey(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		uri := SpaceRecordURI(
			"ats://did:plc:abc123/network.habitat.space/my-space/did:plc:repo456/network.habitat.note/rkey789",
		)
		require.Equal(t, "did:plc:repo456", uri.Repo().String())
		require.Equal(t, "rkey789", uri.Rkey().String())
	})

	t.Run("invalid format returns empty", func(t *testing.T) {
		uri := SpaceRecordURI("not-a-record-uri")
		require.Empty(t, uri.Repo())
		require.Empty(t, uri.Rkey())
	})

	t.Run("space URI without record returns empty", func(t *testing.T) {
		uri := SpaceRecordURI("ats://did:plc:abc123/network.habitat.space/my-space")
		require.Empty(t, uri.Repo())
		require.Empty(t, uri.Rkey())
	})
}

func TestSpaceRecordURI_Collection(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		uri := SpaceRecordURI(
			"ats://did:plc:abc123/network.habitat.space/my-space/did:plc:repo456/network.habitat.note/rkey789",
		)
		require.Equal(t, "network.habitat.note", uri.Collection().String())
	})

	t.Run("invalid format returns empty", func(t *testing.T) {
		uri := SpaceRecordURI("not-a-record-uri")
		require.Empty(t, uri.Collection())
	})

	t.Run("invalid collection NSID returns empty", func(t *testing.T) {
		uri := SpaceRecordURI(
			"ats://did:plc:abc123/network.habitat.space/my-space/did:plc:repo456/not_a_nsid/rkey789",
		)
		require.Empty(t, uri.Collection())
	})

	t.Run("missing trailing segments returns empty", func(t *testing.T) {
		uri := SpaceRecordURI("ats://did:plc:abc123/network.habitat.space/my-space")
		require.Empty(t, uri.Collection())
	})
}

func TestSpaceRecordURI_SpaceURI(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		uri := SpaceRecordURI(
			"ats://did:plc:abc123/network.habitat.space/my-space/did:plc:repo456/network.habitat.note/rkey789",
		)
		require.Equal(
			t,
			SpaceURI("ats://did:plc:abc123/network.habitat.space/my-space"),
			uri.SpaceURI(),
		)
		require.Equal(t, "did:plc:abc123", uri.SpaceOwner().String())
	})

	t.Run("invalid format returns empty", func(t *testing.T) {
		uri := SpaceRecordURI("not-a-record-uri")
		require.Empty(t, uri.SpaceURI())
		require.Empty(t, uri.SpaceOwner())
	})

	t.Run("missing trailing segments returns empty", func(t *testing.T) {
		uri := SpaceRecordURI("ats://did:plc:abc123/network.habitat.space/my-space")
		require.Empty(t, uri.SpaceURI())
		require.Empty(t, uri.SpaceOwner())
	})

	t.Run("invalid owner did returns empty", func(t *testing.T) {
		uri := SpaceRecordURI(
			"ats://not-a-did/network.habitat.space/my-space/did:plc:repo456/network.habitat.note/rkey789",
		)
		require.Empty(t, uri.SpaceURI())
		require.Empty(t, uri.SpaceOwner())
	})
}
