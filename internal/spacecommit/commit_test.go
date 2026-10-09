package spacecommit

import (
	"bytes"
	"context"
	"encoding/hex"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// fakeMember is a MemberSigner backed by a real key, returning ErrDIDNotFound
// when it does not manage the author.
type fakeMember struct {
	key     atcrypto.PrivateKey
	managed bool
}

func (f *fakeMember) PrivateKeyForDID(
	_ context.Context,
	_ syntax.DID,
) (atcrypto.PrivateKey, error) {
	if !f.managed {
		return nil, identity.ErrDIDNotFound
	}
	return f.key, nil
}

func newKey(t *testing.T) (atcrypto.PrivateKey, atcrypto.PublicKey) {
	t.Helper()
	priv, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	pub, err := priv.PublicKey()
	require.NoError(t, err)
	return priv, pub
}

var testSpace = habitat_syntax.ConstructSpaceURI("did:plc:org", "network.habitat.group", "s")

func TestBuild_HabitatSignedForExternalAuthor(t *testing.T) {
	hostKey, hostPub := newKey(t)
	authority := NewAuthority(hostKey, &fakeMember{managed: false})

	author := syntax.DID("did:plc:alice")
	hash := (&LtHash{}).Sum()

	c, err := authority.Build(context.Background(), testSpace, author, "3lart", hash)
	require.NoError(t, err)
	require.Equal(t, Version, c.Ver)
	require.Equal(t, hash, c.Hash)
	require.Equal(t, "3lart", c.Rev)
	require.Len(t, c.Ikm, ikmLen)

	// External authors are host-signed and verify against the host key.
	require.True(t, c.HabitatSigned)
	require.NoError(t, Verify(c, testSpace, author, hash, hostPub))
}

func TestBuild_MemberSignedForManagedAuthor(t *testing.T) {
	memberKey, memberPub := newKey(t)
	hostKey, _ := newKey(t)
	authority := NewAuthority(hostKey, &fakeMember{key: memberKey, managed: true})

	author := syntax.DID("did:web:abc.example.com")
	hash := (&LtHash{}).Sum()

	c, err := authority.Build(context.Background(), testSpace, author, "3lart", hash)
	require.NoError(t, err)

	// Managed authors are signed by their own key, even though a host key is also
	// configured.
	require.False(t, c.HabitatSigned)
	require.NoError(t, Verify(c, testSpace, author, hash, memberPub))
}

func TestBuild_FreshIkmPerCall(t *testing.T) {
	hostKey, _ := newKey(t)
	authority := NewAuthority(hostKey, &fakeMember{managed: false})
	hash := (&LtHash{}).Sum()

	c1, err := authority.Build(context.Background(), testSpace, "did:plc:alice", "3lart", hash)
	require.NoError(t, err)
	c2, err := authority.Build(context.Background(), testSpace, "did:plc:alice", "3lart", hash)
	require.NoError(t, err)

	require.False(t, bytes.Equal(c1.Ikm, c2.Ikm), "each commit uses a fresh ikm")
	require.False(t, bytes.Equal(c1.Sig, c2.Sig), "signatures differ because ctx includes ikm")
}

func TestVerify_RejectsTampering(t *testing.T) {
	hostKey, hostPub := newKey(t)
	authority := NewAuthority(hostKey, &fakeMember{managed: false})
	author := syntax.DID("did:plc:alice")
	hash := (&LtHash{}).Sum()

	c, err := authority.Build(context.Background(), testSpace, author, "3lart", hash)
	require.NoError(t, err)

	// A different recomputed hash than the commit's is rejected.
	other := (&LtHash{})
	other.Add(RecordElement("c", "k", "cid"))
	require.ErrorIs(
		t,
		Verify(c, testSpace, author, other.Sum(), hostPub),
		ErrInvalidCommit,
	)

	// A tampered mac is rejected.
	badMac := c
	badMac.Mac = append([]byte(nil), c.Mac...)
	badMac.Mac[0] ^= 0xff
	require.ErrorIs(
		t,
		Verify(badMac, testSpace, author, hash, hostPub),
		ErrInvalidCommit,
	)

	// The wrong public key is rejected: commits carry no marker for which key
	// signed them, so a caller that guesses wrong just fails verification.
	_, otherPub := newKey(t)
	require.ErrorIs(
		t,
		Verify(c, testSpace, author, hash, otherPub),
		ErrInvalidCommit,
	)
}

// TestVerify_ReferenceCommit verifies a commit signed by @atproto/space's
// RepoCommit.sign over one record, so pear's MAC, ctx and set hash match the
// reference implementation.
func TestVerify_ReferenceCommit(t *testing.T) {
	unhex := func(s string) []byte {
		b, err := hex.DecodeString(s)
		require.NoError(t, err)
		return b
	}
	pub, err := atcrypto.ParsePublicDIDKey(
		"did:key:zQ3shZhBTTmmjTovByuQtnJvR9fwUiVzGjyEC98a2koEoLg2R",
	)
	require.NoError(t, err)

	var h LtHash
	h.Add("com.example.post/abc/bafyreie5737gdxlw5i64vzichcalba3z2v5n6icifvx5xytvske7mr3hpm")
	hash := unhex("6cb32db48754ba72b4328f802ce90a6c9ff84804c41076c0325823c0d034e313")
	require.Equal(t, hash, h.Sum())

	c := SignedCommit{
		Ver:  Version,
		Hash: hash,
		Ikm:  unhex("af34107628358684fe404b35b639cb8aaa3a8671b5947d92962aadddd9bbfe6f"),
		Mac:  unhex("dee40db9c6058291640fac51190ad22038731fedc82cfe2ba9dd23666aa98e4f"),
		Sig: unhex("1974da58ead1b2c5e3ff3dd5fbbd5c2bd333bf87d88fa8d3f79e612726f8559c" +
			"69df0a597205c962403ce6702b30d6f5dca95e8b69c8eb2dc40ac62746fcad70"),
		Rev: "3lart",
	}
	require.NoError(t, Verify(c, testSpace, "did:plc:alice", hash, pub))
}
