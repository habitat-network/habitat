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
	require.Equal(t, SpaceURI("at://did:plc:abc123/space/network.habitat.space/my-space"), uri)
	require.Equal(t, "at://did:plc:abc123/space/network.habitat.space/my-space", uri.String())
	require.Equal(t, "did:plc:abc123", uri.SpaceOwner().String())
	require.Equal(t, "network.habitat.space", uri.SpaceType().String())
	require.Equal(t, SpaceKey("my-space"), uri.Skey())
}

func TestParseSpaceURI(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		uri, err := ParseSpaceURI("at://did:plc:abc123/space/network.habitat.space/my-space_1")
		require.NoError(t, err)
		require.Equal(t, "did:plc:abc123", uri.SpaceOwner().String())
		require.Equal(t, "network.habitat.space", uri.SpaceType().String())
		require.Equal(t, SpaceKey("my-space_1"), uri.Skey())
	})

	t.Run("too long", func(t *testing.T) {
		_, err := ParseSpaceURI(
			"at://did:plc:abc123/space/network.habitat.space/" + strings.Repeat("a", 8193),
		)
		require.Error(t, err)
	})

	t.Run("invalid format", func(t *testing.T) {
		_, err := ParseSpaceURI("habitat://did:plc:abc123/space/network.habitat.space/my-space")
		require.Error(t, err)
	})

	// A legacy URI parses, and is normalized to the current format.
	t.Run("legacy ats format", func(t *testing.T) {
		uri, err := ParseSpaceURI("ats://did:plc:abc123/network.habitat.space/my-space_1")
		require.NoError(t, err)
		require.Equal(
			t,
			SpaceURI("at://did:plc:abc123/space/network.habitat.space/my-space_1"),
			uri,
		)
		require.Equal(t, "did:plc:abc123", uri.SpaceOwner().String())
		require.Equal(t, "network.habitat.space", uri.SpaceType().String())
		require.Equal(t, SpaceKey("my-space_1"), uri.Skey())
	})

	t.Run("missing space segment", func(t *testing.T) {
		_, err := ParseSpaceURI("at://did:plc:abc123/network.habitat.space/my-space")
		require.Error(t, err)
	})

	t.Run("invalid DID", func(t *testing.T) {
		_, err := ParseSpaceURI("at://not-a-did/space/network.habitat.space/my-space")
		require.Error(t, err)
	})

	t.Run("invalid type", func(t *testing.T) {
		_, err := ParseSpaceURI("at://did:plc:abc123/space/not_a_nsid/my-space")
		require.Error(t, err)
	})
}

func TestSpaceURIAccessorsReturnEmptyForInvalidURI(t *testing.T) {
	uri := SpaceURI("not-a-space-uri")
	require.Empty(t, uri.SpaceOwner())
	require.Empty(t, uri.SpaceType())
	require.Empty(t, uri.Skey())

	uri = SpaceURI("at://not-a-did/space/network.habitat.space/my-space")
	require.Empty(t, uri.SpaceOwner())
	require.Equal(t, "network.habitat.space", uri.SpaceType().String())
	require.Equal(t, SpaceKey("my-space"), uri.Skey())

	uri = SpaceURI("at://did:plc:abc123/space/not_a_nsid/my-space")
	require.Empty(t, uri.SpaceType())
}

func TestSpaceURICanonicalAndLegacy(t *testing.T) {
	current := SpaceURI("at://did:plc:abc123/space/network.habitat.space/my-space")
	legacy := SpaceURI("ats://did:plc:abc123/network.habitat.space/my-space")

	// Both formats convert to either format, and converting is idempotent.
	for _, uri := range []SpaceURI{current, legacy} {
		require.Equal(t, current, uri.Canonical())
		require.Equal(t, legacy, uri.Legacy())
	}

	// Unparseable URIs pass through untouched rather than collapsing to a
	// well-formed-looking URI with empty components.
	garbage := SpaceURI("not-a-space-uri")
	require.Equal(t, garbage, garbage.Canonical())
	require.Equal(t, garbage, garbage.Legacy())
}

func TestConstructSpaceRecordURI(t *testing.T) {
	uri := ConstructSpaceRecordURI(
		"at://did:plc:abc123/space/network.habitat.space/my-space",
		"did:plc:repo456",
		"network.habitat.note",
		"rkey789",
	)
	require.Equal(
		t,
		SpaceRecordURI(
			"at://did:plc:abc123/space/network.habitat.space/my-space/did:plc:repo456/network.habitat.note/rkey789",
		),
		uri,
	)
	require.Equal(
		t,
		"at://did:plc:abc123/space/network.habitat.space/my-space/did:plc:repo456/network.habitat.note/rkey789",
		uri.String(),
	)
	require.Equal(t, "network.habitat.note", uri.Collection().String())
}

func TestParseSpaceRecordURI(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		uri, err := ParseSpaceRecordURI(
			"at://did:plc:abc123/space/network.habitat.space/my-space/did:plc:repo456/network.habitat.note/rkey789",
		)
		require.NoError(t, err)
		require.Equal(t, "network.habitat.note", uri.Collection().String())
		require.Equal(t, "did:plc:repo456", uri.Repo().String())
		require.Equal(t, "rkey789", uri.Rkey().String())
		require.Equal(
			t,
			SpaceURI("at://did:plc:abc123/space/network.habitat.space/my-space"),
			uri.SpaceURI(),
		)
	})

	t.Run("too long", func(t *testing.T) {
		_, err := ParseSpaceRecordURI(
			"at://did:plc:abc123/space/network.habitat.space/my-space/did:plc:repo456/network.habitat.note/" +
				strings.Repeat(
					"a",
					8193,
				),
		)
		require.Error(t, err)
	})

	t.Run("invalid format", func(t *testing.T) {
		_, err := ParseSpaceRecordURI("at://did:plc:abc123/space/network.habitat.space/my-space")
		require.Error(t, err)
	})

	// A legacy URI parses, and is normalized to the current format.
	t.Run("legacy ats format", func(t *testing.T) {
		uri, err := ParseSpaceRecordURI(
			"ats://did:plc:abc123/network.habitat.space/my-space/did:plc:repo456/network.habitat.note/rkey789",
		)
		require.NoError(t, err)
		require.Equal(
			t,
			SpaceRecordURI(
				"at://did:plc:abc123/space/network.habitat.space/my-space/did:plc:repo456/network.habitat.note/rkey789",
			),
			uri,
		)
	})

	t.Run("invalid DID", func(t *testing.T) {
		_, err := ParseSpaceRecordURI(
			"at://not-a-did/space/network.habitat.space/my-space/did:plc:repo456/network.habitat.note/rkey789",
		)
		require.Error(t, err)
	})

	t.Run("invalid type", func(t *testing.T) {
		_, err := ParseSpaceRecordURI(
			"at://did:plc:abc123/space/not_a_nsid/my-space/did:plc:repo456/network.habitat.note/rkey789",
		)
		require.Error(t, err)
	})

	t.Run("invalid repo DID", func(t *testing.T) {
		_, err := ParseSpaceRecordURI(
			"at://did:plc:abc123/space/network.habitat.space/my-space/not-a-did/network.habitat.note/rkey789",
		)
		require.Error(t, err)
	})

	t.Run("invalid collection NSID", func(t *testing.T) {
		_, err := ParseSpaceRecordURI(
			"at://did:plc:abc123/space/network.habitat.space/my-space/did:plc:repo456/not_a_nsid/rkey789",
		)
		require.Error(t, err)
	})
}

func TestSpaceRecordURI_Collection(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		uri := SpaceRecordURI(
			"at://did:plc:abc123/space/network.habitat.space/my-space/did:plc:repo456/network.habitat.note/rkey789",
		)
		require.Equal(t, "network.habitat.note", uri.Collection().String())
	})

	t.Run("invalid format returns empty", func(t *testing.T) {
		uri := SpaceRecordURI("not-a-record-uri")
		require.Empty(t, uri.Collection())
	})

	t.Run("invalid collection NSID returns empty", func(t *testing.T) {
		uri := SpaceRecordURI(
			"at://did:plc:abc123/space/network.habitat.space/my-space/did:plc:repo456/not_a_nsid/rkey789",
		)
		require.Empty(t, uri.Collection())
	})

	t.Run("missing trailing segments returns empty", func(t *testing.T) {
		uri := SpaceRecordURI("at://did:plc:abc123/space/network.habitat.space/my-space")
		require.Empty(t, uri.Collection())
	})
}

func TestSpaceRecordURI_SpaceURI(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		uri := SpaceRecordURI(
			"at://did:plc:abc123/space/network.habitat.space/my-space/did:plc:repo456/network.habitat.note/rkey789",
		)
		require.Equal(
			t,
			SpaceURI("at://did:plc:abc123/space/network.habitat.space/my-space"),
			uri.SpaceURI(),
		)
		require.Equal(t, "did:plc:abc123", uri.SpaceOwner().String())
	})

	// A legacy record URI still parses, and normalizes to a current-format
	// space URI.
	t.Run("legacy ats format", func(t *testing.T) {
		uri := SpaceRecordURI(
			"ats://did:plc:abc123/network.habitat.space/my-space/did:plc:repo456/network.habitat.note/rkey789",
		)
		require.Equal(
			t,
			SpaceURI("at://did:plc:abc123/space/network.habitat.space/my-space"),
			uri.SpaceURI(),
		)
		require.Equal(t, "did:plc:abc123", uri.SpaceOwner().String())
		require.Equal(t, "network.habitat.note", uri.Collection().String())
		require.Equal(t, "did:plc:repo456", uri.Repo().String())
		require.Equal(t, "rkey789", uri.Rkey().String())
	})

	t.Run("invalid format returns empty", func(t *testing.T) {
		uri := SpaceRecordURI("not-a-record-uri")
		require.Empty(t, uri.SpaceURI())
		require.Empty(t, uri.SpaceOwner())
	})

	t.Run("missing trailing segments returns empty", func(t *testing.T) {
		uri := SpaceRecordURI("at://did:plc:abc123/space/network.habitat.space/my-space")
		require.Empty(t, uri.SpaceURI())
		require.Empty(t, uri.SpaceOwner())
	})

	t.Run("invalid owner did returns empty", func(t *testing.T) {
		uri := SpaceRecordURI(
			"at://not-a-did/space/network.habitat.space/my-space/did:plc:repo456/network.habitat.note/rkey789",
		)
		require.Empty(t, uri.SpaceURI())
		require.Empty(t, uri.SpaceOwner())
	})
}
