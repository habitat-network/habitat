package spaces_test

import (
	"bytes"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/spaces"
	"github.com/habitat-network/habitat/internal/star"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const metaSpace = habitat_syntax.SpaceURI("at://did:plc:authority/space/network.habitat.group/main")

func sampleMeta() spaces.SpaceMeta {
	return spaces.SpaceMeta{
		Space: metaSpace,
		Policy: spaces.Policy{
			Type:        spaces.PolicyMemberSpace,
			PublicRead:  true,
			AllowedApps: []string{"https://app.test"},
		},
		// Out of key order, and one with a localhost port, which is a
		// percent-encoded colon in a DID but not in a record key.
		Members: []syntax.DID{"did:plc:zed", "did:plc:alice", "did:web:localhost%3A2583"},
		Writers: []spaces.MetaWriter{
			{
				DID: "did:plc:zed", RepoRev: "3kabcdefghij2", Data: bytes.Repeat([]byte{2}, 32),
				SpaceRev: "3kabcdefghij3",
			},
			{
				DID: "did:plc:alice", RepoRev: "3kabcdefghij4", Data: bytes.Repeat([]byte{1}, 32),
				SpaceRev: "3kabcdefghij5",
			},
		},
	}
}

// metaPairs reads an archive's keys in order.
func metaPairs(t *testing.T, archive []byte) (map[string]any, []string) {
	t.Helper()
	r, err := star.NewReader(bytes.NewReader(archive), 0)
	require.NoError(t, err)
	var keys []string
	for {
		k, _, err := r.Next()
		if err != nil {
			break
		}
		keys = append(keys, string(k))
	}
	return r.Meta, keys
}

func TestSpaceMetaStar_RoundTrip(t *testing.T) {
	t.Parallel()
	archive, err := spaces.WriteSpaceMetaStar(sampleMeta())
	require.NoError(t, err)

	meta, keys := metaPairs(t, archive)
	require.Equal(t, spaces.SpaceMetaStarType, meta["$type"])
	require.Equal(t, int64(1), meta["ver"])
	require.Equal(t, metaSpace.String(), meta["space"])
	// The policy, then the member state, then the writers, each in key order.
	require.Equal(t, []string{
		"policy",
		"com.atproto.memberspace.member/did:plc:alice",
		"com.atproto.memberspace.member/did:plc:zed",
		"com.atproto.memberspace.member/did:web:localhost:2583",
		"writer/did:plc:alice",
		"writer/did:plc:zed",
	}, keys)

	got, err := spaces.ReadSpaceMetaStar(bytes.NewReader(archive))
	require.NoError(t, err)
	require.Equal(t, metaSpace, got.Space)
	require.Equal(t, sampleMeta().Policy, got.Policy)
	require.Equal(t, []syntax.DID{
		"did:plc:alice", "did:plc:zed", "did:web:localhost%3A2583",
	}, got.Members)
	require.Equal(
		t,
		[]spaces.MetaWriter{sampleMeta().Writers[1], sampleMeta().Writers[0]},
		got.Writers,
	)
}

func TestSpaceMetaStar_NonMemberspacePolicies(t *testing.T) {
	t.Parallel()
	for _, policy := range []string{spaces.PolicyPersonalSpace, spaces.PolicyRelationSpace} {
		archive, err := spaces.WriteSpaceMetaStar(spaces.SpaceMeta{
			Space: metaSpace, Policy: spaces.Policy{Type: policy},
		})
		require.NoError(t, err)
		got, err := spaces.ReadSpaceMetaStar(bytes.NewReader(archive))
		require.NoError(t, err)
		require.Equal(t, policy, got.Policy.Type)
		require.Empty(t, got.Members)
		require.Empty(t, got.Writers)
	}
}

// metaArchive builds an archive from raw pairs, to craft invalid ones.
func metaArchive(t *testing.T, meta map[string]any, pairs ...[2]any) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := star.NewWriter(&buf, meta)
	require.NoError(t, err)
	for _, p := range pairs {
		value, err := atdata.MarshalCBOR(p[1].(map[string]any))
		require.NoError(t, err)
		require.NoError(t, w.WritePair([]byte(p[0].(string)), value))
	}
	return buf.Bytes()
}

func TestReadSpaceMetaStar_Rejects(t *testing.T) {
	t.Parallel()
	meta := map[string]any{
		"$type": spaces.SpaceMetaStarType, "ver": int64(1), "space": metaSpace.String(),
	}
	policy := [2]any{"policy", map[string]any{"$type": spaces.PolicyMemberSpace}}
	member := func(did string, read, write bool) [2]any {
		return [2]any{
			"com.atproto.memberspace.member/" + did,
			map[string]any{"$type": "com.atproto.memberspace.member", "read": read, "write": write},
		}
	}
	writer := func(did string, edit func(m map[string]any)) [2]any {
		m := map[string]any{
			"repoRev": "3kabcdefghij2", "data": atdata.Bytes{1}, "spaceRev": "3kabcdefghij3",
		}
		edit(m)
		return [2]any{"writer/" + did, m}
	}
	noEdit := func(map[string]any) {}
	with := func(edit func(m map[string]any)) map[string]any {
		m := map[string]any{}
		for k, v := range meta {
			m[k] = v
		}
		edit(m)
		return m
	}

	tests := []struct {
		name    string
		archive []byte
	}{
		{"not a star archive", []byte("nope")},
		{
			"wrong profile",
			metaArchive(
				t,
				with(func(m map[string]any) { m["$type"] = "atproto-space-repo" }),
				policy,
			),
		},
		{
			"unsupported version",
			metaArchive(t, with(func(m map[string]any) { m["ver"] = int64(2) }), policy),
		},
		{"missing policy", metaArchive(t, meta, writer("did:plc:a", noEdit))},
		{"state under a policy without any", metaArchive(t, meta,
			[2]any{"policy", map[string]any{"$type": spaces.PolicyPersonalSpace}},
			member("did:plc:a", true, true))},
		{"unrecognized policy state", metaArchive(t, meta, policy,
			[2]any{"com.example.thing/x", map[string]any{"a": int64(1)}})},
		{"member without write", metaArchive(t, meta, policy, member("did:plc:a", true, false))},
		{"member with a bad did", metaArchive(t, meta, policy, member("nope", true, true))},
		{"writers before members", metaArchive(t, meta, policy,
			writer("did:plc:a", noEdit), member("did:plc:b", true, true))},
		{"writers out of order", metaArchive(t, meta, policy,
			writer("did:plc:b", noEdit), writer("did:plc:a", noEdit))},
		{"duplicate writer", metaArchive(t, meta, policy,
			writer("did:plc:a", noEdit), writer("did:plc:a", noEdit))},
		{"writer without data", metaArchive(t, meta, policy,
			writer("did:plc:a", func(m map[string]any) { delete(m, "data") }))},
		{"writer with a bad rev", metaArchive(t, meta, policy,
			writer("did:plc:a", func(m map[string]any) { m["repoRev"] = "x" }))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := spaces.ReadSpaceMetaStar(bytes.NewReader(tt.archive))
			require.ErrorIs(t, err, spaces.ErrInvalidSpaceMeta)
		})
	}

	t.Run("an unsupported policy is reported as such", func(t *testing.T) {
		archive := metaArchive(t, meta,
			[2]any{"policy", map[string]any{"$type": spaces.PolicyManagedSpace}})
		_, err := spaces.ReadSpaceMetaStar(bytes.NewReader(archive))
		require.ErrorIs(t, err, spaces.ErrUnsupportedPolicy)
	})
}
