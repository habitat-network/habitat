package spacecommit

import (
	"bytes"
	"context"
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

func TestBuild_HostSignedForExternalAuthor(t *testing.T) {
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

	// External authors are host-signed under the host tag and verify against the
	// host key.
	require.NoError(t, Verify(c, HostProtocolTag, testSpace, author, hash, hostPub))
}

func TestBuild_MemberSignedForManagedAuthor(t *testing.T) {
	memberKey, memberPub := newKey(t)
	hostKey, _ := newKey(t)
	authority := NewAuthority(hostKey, &fakeMember{key: memberKey, managed: true})

	author := syntax.DID("did:web:abc.example.com")
	hash := (&LtHash{}).Sum()

	c, err := authority.Build(context.Background(), testSpace, author, "3lart", hash)
	require.NoError(t, err)

	// Managed authors are signed by their own key under the spec tag, even though
	// a host key is also configured.
	require.NoError(t, Verify(c, SpecProtocolTag, testSpace, author, hash, memberPub))
}

func TestBuild_NoSignerFails(t *testing.T) {
	authority := NewAuthority(nil, &fakeMember{managed: false})
	_, err := authority.Build(
		context.Background(),
		testSpace,
		"did:plc:alice",
		"3lart",
		(&LtHash{}).Sum(),
	)
	require.ErrorIs(t, err, ErrNoSigner)
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
		Verify(c, HostProtocolTag, testSpace, author, other.Sum(), hostPub),
		ErrInvalidCommit,
	)

	// A tampered mac is rejected.
	badMac := c
	badMac.Mac = append([]byte(nil), c.Mac...)
	badMac.Mac[0] ^= 0xff
	require.ErrorIs(
		t,
		Verify(badMac, HostProtocolTag, testSpace, author, hash, hostPub),
		ErrInvalidCommit,
	)

	// The wrong tag (verifying a host-signed commit as spec) is rejected.
	require.ErrorIs(
		t,
		Verify(c, SpecProtocolTag, testSpace, author, hash, hostPub),
		ErrInvalidCommit,
	)
}
