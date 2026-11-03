package spacecommit

import (
	"bytes"
	"context"
	"encoding/hex"
	"net/http"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Reference vectors computed with @atproto/space 0.0.0-spaces-alpha-20261029171913
// (decapsulate, computeCommitMac, computeRecordMac, formatWantMacHeader,
// formatMacHeader), so pear and sap interoperate with the reference syncer and
// repo host.
var (
	refSpace = habitat_syntax.SpaceURI(
		"at://did:web:org.example.com/space/com.example.space/main",
	)
	refRepo       = syntax.DID("did:web:alice.example.com")
	refRev        = "3l4qjqc2cak2a"
	refData       = bytes.Repeat([]byte{7}, 32)
	refKeyShare   = "did:key:zDnaex62me84JZnkEzmeYRa8FCLNe7y1asoSwBMK26GBYpL7c"
	refSecret     = "76bccf1264f5f7f5b7df0b78b6c1037f2e22fb513288207ad2dede881710d4c3"
	refCommitMAC  = "ddc2ab46552b23e4a419d517efa25a4fb9a1e4abaeb7d16d87a6af1cb2d25f8e"
	refRecordMAC  = "14f998c8ca32ac0ee2c9a8e86da19609443940af7c25afba8903c3a323198964"
	refRecordCID  = "bafyreie5737gdxlw5i64vzichcalba3z2v5n6icifvx5xytvske7mr3hpm"
	refWantHeader = `type="atproto-space-repo", ver=1, key_share="` + refKeyShare + `"`
	refMACHeader  = `type="atproto-space-repo", ver=1, mac=:3cKrRlUrI+SkGdUX76JaT7mh5Kuut9Fth6avHLLSX44=:`
)

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
}

// refAuthorKEM is the reference author's KEM keypair (private key 0x11 * 32).
func refAuthorKEM(t *testing.T) *atcrypto.PrivateKeyP256 {
	t.Helper()
	key, err := atcrypto.ParsePrivateBytesP256(bytes.Repeat([]byte{0x11}, 32))
	require.NoError(t, err)
	return key
}

func TestCommitMACMatchesReference(t *testing.T) {
	secret, err := Decapsulate(refAuthorKEM(t), refKeyShare)
	require.NoError(t, err)
	require.Equal(t, unhex(t, refSecret), secret)

	c := New(refSpace, refRepo, refRev, refData)
	require.Equal(t, unhex(t, refCommitMAC), c.MAC(secret))
	require.NoError(t, c.Verify(refSpace, refRepo, secret, unhex(t, refCommitMAC)))

	recordURI := habitat_syntax.ConstructSpaceRecordURI(
		refSpace, refRepo, "com.example.record", "abc",
	).String()
	recordCID := cid.MustParse(refRecordCID)
	require.Equal(t, unhex(t, refRecordMAC), RecordMAC(secret, recordURI, recordCID))
	require.NoError(t, VerifyRecordMAC(secret, recordURI, recordCID, unhex(t, refRecordMAC)))
}

func TestCommitVerifyRejectsMismatches(t *testing.T) {
	secret := unhex(t, refSecret)
	c := New(refSpace, refRepo, refRev, refData)
	mac := c.MAC(secret)
	other := habitat_syntax.SpaceURI("at://did:web:org.example.com/space/com.example.space/other")

	cases := map[string]func() error{
		"wrong repo":   func() error { return c.Verify(refSpace, "did:web:bob.example.com", secret, mac) },
		"wrong space":  func() error { return c.Verify(other, refRepo, secret, mac) },
		"wrong secret": func() error { return c.Verify(refSpace, refRepo, bytes.Repeat([]byte{1}, 32), mac) },
		"tampered data": func() error {
			tampered := c
			tampered.Data = bytes.Repeat([]byte{8}, 32)
			return tampered.Verify(refSpace, refRepo, secret, mac)
		},
		"unsupported version": func() error {
			v2 := c
			v2.Ver = 2
			return v2.Verify(refSpace, refRepo, secret, v2.MAC(secret))
		},
	}
	for name, verify := range cases {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, verify(), ErrInvalidCommit)
		})
	}

	recordURI := habitat_syntax.ConstructSpaceRecordURI(refSpace, refRepo, "com.example.record", "abc")
	recordMAC := RecordMAC(secret, recordURI.String(), cid.MustParse(refRecordCID))
	otherCID := cid.MustParse("bafyreihffx5a2e7k5uwrmmgofbvzujc5cmw5h4espouwuxt3liqoflx3ee")
	require.ErrorIs(t,
		VerifyRecordMAC(secret, recordURI.String(), otherCID, recordMAC), ErrInvalidCommit)
}

func TestCommitMatches(t *testing.T) {
	var lt LtHash
	lt.Add(RecordElement("com.example.post", "abc", refRecordCID))
	c := New(refSpace, refRepo, refRev, lt.Sum())
	require.True(t, c.Matches(&lt))
	require.False(t, c.Matches(&LtHash{}))
}

func TestMACHeadersMatchReference(t *testing.T) {
	want, err := FormatWantMAC(CommitMACType, refKeyShare)
	require.NoError(t, err)
	require.Equal(t, refWantHeader, want)

	h := http.Header{}
	require.NoError(t, SetMAC(h, CommitMACType, unhex(t, refCommitMAC)))
	require.Equal(t, refMACHeader, h.Get(MACHeader))
	require.Equal(t, WantMACHeader, h.Get("Vary"))

	mac, err := ParseMAC(h, CommitMACType)
	require.NoError(t, err)
	require.Equal(t, unhex(t, refCommitMAC), mac)

	req := http.Header{}
	req.Set(WantMACHeader, refWantHeader)
	keyShare, ok, err := ParseWantMAC(req, CommitMACType)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, refKeyShare, keyShare)
}

func TestMACHeadersRejectMalformed(t *testing.T) {
	_, ok, err := ParseWantMAC(http.Header{}, CommitMACType)
	require.NoError(t, err)
	require.False(t, ok, "an absent header is not an error")

	for name, value := range map[string]string{
		"not a dictionary": `"just a string"`,
		"wrong type":       `type="atproto-space-record", ver=1, key_share="did:key:z"`,
		"wrong version":    `type="atproto-space-repo", ver=2, key_share="did:key:z"`,
		"missing share":    `type="atproto-space-repo", ver=1`,
		"inner list":       `type="atproto-space-repo", ver=1, key_share=("a" "b")`,
	} {
		t.Run(name, func(t *testing.T) {
			h := http.Header{}
			h.Set(WantMACHeader, value)
			_, ok, err := ParseWantMAC(h, CommitMACType)
			require.True(t, ok)
			require.ErrorIs(t, err, ErrBadSpaceMAC)
		})
	}

	_, err = ParseMAC(http.Header{}, RecordMACType)
	require.ErrorIs(t, err, ErrBadSpaceMAC, "a missing response header is an error")
	h := http.Header{}
	h.Set(MACHeader, `type="atproto-space-record", ver=1, mac="not bytes"`)
	_, err = ParseMAC(h, RecordMACType)
	require.ErrorIs(t, err, ErrBadSpaceMAC)
}

func TestEncapsulateDecapsulateRoundTrip(t *testing.T) {
	author, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	pub, err := author.PublicKey()
	require.NoError(t, err)

	keyShare, secret, err := Encapsulate(pub)
	require.NoError(t, err)
	got, err := Decapsulate(author, keyShare)
	require.NoError(t, err)
	require.Equal(t, secret, got)

	// Every encapsulation uses a fresh ephemeral key.
	keyShare2, secret2, err := Encapsulate(pub)
	require.NoError(t, err)
	require.NotEqual(t, keyShare, keyShare2)
	require.NotEqual(t, secret, secret2)
}

func TestKEMRejectsInvalidKeys(t *testing.T) {
	k256, err := atcrypto.GeneratePrivateKeyK256()
	require.NoError(t, err)
	k256Pub, err := k256.PublicKey()
	require.NoError(t, err)

	_, _, err = Encapsulate(k256Pub)
	require.ErrorIs(t, err, ErrBadSpaceMAC, "KEM keys are P-256")
	_, err = Decapsulate(refAuthorKEM(t), "not-a-did-key")
	require.ErrorIs(t, err, ErrBadSpaceMAC)
	_, err = Decapsulate(refAuthorKEM(t), k256Pub.DIDKey())
	require.ErrorIs(t, err, ErrBadSpaceMAC)
}

func TestDeriveKEMKeyIsDeterministicAndDedicated(t *testing.T) {
	seed, err := atcrypto.GeneratePrivateKeyK256()
	require.NoError(t, err)
	a, err := DeriveKEMKey(seed)
	require.NoError(t, err)
	b, err := DeriveKEMKey(seed)
	require.NoError(t, err)
	require.True(t, a.Equal(b))
	require.NotEqual(t, seed.Bytes(), a.Bytes())
}

// fakeMemberKEM is a MemberKEMKeys backed by one key, returning
// ErrDIDNotFound for every DID but the one it manages.
type fakeMemberKEM struct {
	did syntax.DID
	key *atcrypto.PrivateKeyP256
}

func (f fakeMemberKEM) KEMKeyForDID(
	_ context.Context, did syntax.DID,
) (*atcrypto.PrivateKeyP256, error) {
	if did != f.did {
		return nil, identity.ErrDIDNotFound
	}
	return f.key, nil
}

func TestKEMKeysPicksMemberKeyElseHostKey(t *testing.T) {
	member, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	host, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	keys := NewKEMKeys(host, fakeMemberKEM{did: refRepo, key: member})

	for name, tc := range map[string]struct {
		author syntax.DID
		key    *atcrypto.PrivateKeyP256
	}{
		"managed author uses their own key": {refRepo, member},
		"external author uses the host key": {"did:plc:external", host},
	} {
		t.Run(name, func(t *testing.T) {
			pub, err := tc.key.PublicKey()
			require.NoError(t, err)
			keyShare, secret, err := Encapsulate(pub)
			require.NoError(t, err)
			got, err := keys.Decapsulate(t.Context(), tc.author, keyShare)
			require.NoError(t, err)
			require.Equal(t, secret, got)
		})
	}

	noHost := NewKEMKeys(nil, fakeMemberKEM{did: refRepo, key: member})
	_, err = noHost.Decapsulate(t.Context(), "did:plc:external", refKeyShare)
	require.ErrorIs(t, err, ErrBadSpaceMAC)
}
