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

func TestParseSpaceRecordURI(t *testing.T) {
	t.Run("round trip", func(t *testing.T) {
		space := ConstructSpaceURI("did:plc:abc123", "network.habitat.space", "my-space")
		want := ConstructSpaceRecordURI(
			space,
			"did:plc:owner",
			"network.habitat.relationship.tuple",
			"rkey1",
		)

		uri, gotSpace, repo, collection, rkey, err := ParseSpaceRecordURI(want.String())
		require.NoError(t, err)
		require.Equal(t, want, uri)
		require.Equal(t, space, gotSpace)
		require.Equal(t, "did:plc:owner", repo.String())
		require.Equal(t, "network.habitat.relationship.tuple", collection.String())
		require.Equal(t, "rkey1", rkey.String())
	})

	t.Run("wrong scheme", func(t *testing.T) {
		_, _, _, _, _, err := ParseSpaceRecordURI(
			"https://did:plc:abc/type/skey/did:plc:owner/coll/rkey",
		)
		require.Error(t, err)
	})

	t.Run("too few segments", func(t *testing.T) {
		_, _, _, _, _, err := ParseSpaceRecordURI("ats://did:plc:abc/type/skey/did:plc:owner")
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
