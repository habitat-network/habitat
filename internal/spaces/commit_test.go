package spaces

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"io"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/hkdf"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// fakeMember is a MemberSigner backed by a real key, so tests can verify member
// signatures.
type fakeMember struct {
	priv    atcrypto.PrivateKey
	managed bool
}

func (f *fakeMember) PrivateKeyForDID(
	ctx context.Context,
	did syntax.DID,
) (atcrypto.PrivateKey, error) {
	if f.managed {
		return f.priv, nil
	}
	return nil, identity.ErrDIDNotFound
}

func newPrivateKeyForTest(t *testing.T) atcrypto.PrivateKey {
	t.Helper()
	priv, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	return priv
}

// verifyCommit reconstructs the ctx from the commit's own fields and checks the
// mac and the signature against pub, using the given tag.
func verifyCommit(
	t *testing.T,
	c signedCommit,
	tag string,
	space habitat_syntax.SpaceURI,
	author syntax.DID,
	pub atcrypto.PublicKey,
) {
	t.Helper()
	ctx := commitCtx(tag, space, author, c.Rev, c.Ikm)

	macKey := make([]byte, sha256.Size)
	_, err := io.ReadFull(hkdf.New(sha256.New, c.Ikm, nil, ctx), macKey)
	require.NoError(t, err)
	m := hmac.New(sha256.New, macKey)
	_, _ = m.Write(c.Hash)
	require.True(t, hmac.Equal(m.Sum(nil), c.Mac), "mac must bind hash to ctx")

	require.NoError(t, pub.HashAndVerify(ctx, c.Sig), "sig must verify over ctx")
}

func TestBuildSignedCommit_HostSignedForExternalAuthor(t *testing.T) {
	host := newPrivateKeyForTest(t)
	authority := &commitAuthority{host: newPrivateKeyForTest(t), member: nil}

	space := habitat_syntax.ConstructSpaceURI("did:plc:org", "network.habitat.space", "s")
	author := syntax.DID("did:plc:alice") // external, not habitat-managed
	hash := (&ltHash{}).sum()

	c, err := buildSignedCommit(context.Background(), authority, space, author, "3lart", hash)
	require.NoError(t, err)
	require.Equal(t, commitVersion, c.Ver)
	require.Equal(t, hash, c.Hash)
	require.Equal(t, "3lart", c.Rev)
	require.Len(t, c.Ikm, commitIKMLen)

	// External authors are signed by the host key under the host protocol tag.
	pub, err := host.PublicKey()
	verifyCommit(t, c, hostProtocolTag, space, author, pub)
}

func TestBuildSignedCommit_MemberSignedForManagedAuthor(t *testing.T) {
	priv, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	memberPub, err := priv.PublicKey()
	require.NoError(t, err)

	authority := &commitAuthority{
		host:   newPrivateKeyForTest(t),
		member: &fakeMember{priv: priv, managed: true},
	}

	space := habitat_syntax.ConstructSpaceURI("did:plc:org", "network.habitat.space", "s")
	author := syntax.DID("did:web:abc.example.com")
	hash := (&ltHash{}).sum()

	c, err := buildSignedCommit(context.Background(), authority, space, author, "3lart", hash)
	require.NoError(t, err)

	// Habitat-managed authors are signed by their own key under the spec tag,
	// even though a host signer is also configured.
	verifyCommit(t, c, specProtocolTag, space, author, memberPub)
}

func TestBuildSignedCommit_NoSignerFails(t *testing.T) {
	authority := &commitAuthority{}
	space := habitat_syntax.ConstructSpaceURI("did:plc:org", "network.habitat.space", "s")
	_, err := buildSignedCommit(
		context.Background(),
		authority,
		space,
		"did:plc:alice",
		"3lart",
		(&ltHash{}).sum(),
	)
	require.ErrorIs(t, err, errNoCommitSigner)
}

func TestBuildSignedCommit_FreshIkmPerCall(t *testing.T) {
	authority := &commitAuthority{host: newPrivateKeyForTest(t)}
	space := habitat_syntax.ConstructSpaceURI("did:plc:org", "network.habitat.space", "s")
	hash := (&ltHash{}).sum()

	c1, err := buildSignedCommit(
		context.Background(),
		authority,
		space,
		"did:plc:alice",
		"3lart",
		hash,
	)
	require.NoError(t, err)
	c2, err := buildSignedCommit(
		context.Background(),
		authority,
		space,
		"did:plc:alice",
		"3lart",
		hash,
	)
	require.NoError(t, err)

	require.False(t, bytes.Equal(c1.Ikm, c2.Ikm), "each commit uses a fresh ikm")
	require.False(t, bytes.Equal(c1.Sig, c2.Sig), "signatures differ because ctx includes ikm")
}
