package spacecommit

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
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

var (
	refSpace = habitat_syntax.SpaceURI(
		"at://did:web:org.example.com/space/com.example.space/main",
	)
	refRepo      = syntax.DID("did:web:alice.example.com")
	refRev       = "3l4qjqc2cak2a"
	refData      = bytes.Repeat([]byte{1}, 32)
	refRecordCID = "bafyreie5737gdxlw5i64vzichcalba3z2v5n6icifvx5xytvske7mr3hpm"
	// Shared secret and key share of the reference syncer's request.
	refKeyShare = "did:key:zDnaExample"
)

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
}

// refAuthorKEM is a fixed author KEM keypair.
func refAuthorKEM(t *testing.T) *atcrypto.PrivateKeyP256 {
	t.Helper()
	key, err := atcrypto.ParsePrivateBytesP256(bytes.Repeat([]byte{0x11}, 32))
	require.NoError(t, err)
	return key
}

// TestDecapsulateMatchesHPKESecretExport uses the export-only DHKEM(P-256,
// HKDF-SHA256) entry of the HPKE test vectors, as @atproto/space's kem test
// does: enc is the syncer's key share and skRm the author's KEM key. The
// expected secret was computed with Go's crypto/hpke with info "atproto-mac"
// and an empty exporter context.
func TestDecapsulateMatchesHPKESecretExport(t *testing.T) {
	enc := unhex(t, "04d804370b7e24b94749eb1dc8df6d4d4a5d75f9effad01739ebcad5c54a40d57a"+
		"aa8b4190fc124dbde2e4f1e1d1b012a3bc4038157dc29b55533a932306d8d38d")
	skRm := unhex(t, "62c3868357a464f8461d03aa0182c7cebcde841036aea7230ddc7339f1088346")
	want := unhex(t, "2060e0558d1b34c10086bc137f849bb79ba59fcdc30544418e35c9cd301f0d0d")

	kem, err := atcrypto.ParsePrivateBytesP256(skRm)
	require.NoError(t, err)
	share, err := atcrypto.ParsePublicUncompressedBytesP256(enc)
	require.NoError(t, err)
	got, err := Decapsulate(kem, share.DIDKey())
	require.NoError(t, err)
	require.Equal(t, want, got)
}

var (
	refCommitRequest = `mac=("atproto-space-commit";sf);keyid="` + refKeyShare +
		`";tag="atproto-space-commit"`
	refCommitParams = refCommitRequest[len("mac="):]
)

func refMACRequest(t *testing.T) *MACRequest {
	t.Helper()
	h := http.Header{}
	h.Set(AcceptSignatureHeader, refCommitRequest)
	req, err := ParseMACRequest(h, CommitMACType)
	require.NoError(t, err)
	require.NotNil(t, req)
	return req
}

func TestCommitMACHeadersFollowRFC9421(t *testing.T) {
	secret := bytes.Repeat([]byte{9}, 32)
	require.Equal(t, refCommitRequest, FormatMACRequest(CommitMACType, refKeyShare))
	req := refMACRequest(t)
	require.Equal(t, &MACRequest{
		Label: "mac", KeyShare: refKeyShare, SignatureParams: refCommitParams,
	}, req)

	c := New(refSpace, refRepo, refRev, refData)
	h := http.Header{}
	require.NoError(t, SetCommitMAC(h, c, *req, secret))

	covered := `ver=1, repo="did:web:alice.example.com", ` +
		`space="at://did:web:org.example.com/space/com.example.space/main", ` +
		`rev="3l4qjqc2cak2a", data=:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=:`
	require.Equal(t, covered, h.Get("Atproto-Space-Commit"))
	require.Equal(t, "mac="+refCommitParams, h.Get("Signature-Input"))

	// An independent computation of the RFC 9421 signature base.
	base := "\"atproto-space-commit\";sf: " + covered +
		"\n\"@signature-params\": " + refCommitParams
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(base))
	require.Equal(t, "mac=:"+base64.StdEncoding.EncodeToString(m.Sum(nil))+":", h.Get("Signature"))

	require.NoError(t, c.Verify(refSpace, refRepo, h, refKeyShare, secret))
}

func TestCommitVerifyRejectsMismatches(t *testing.T) {
	secret := bytes.Repeat([]byte{9}, 32)
	c := New(refSpace, refRepo, refRev, refData)
	h := http.Header{}
	require.NoError(t, SetCommitMAC(h, c, *refMACRequest(t), secret))
	other := habitat_syntax.SpaceURI("at://did:web:org.example.com/space/com.example.space/other")

	cases := map[string]func() error{
		"wrong repo":  func() error { return c.Verify(refSpace, "did:web:bob.example.com", h, refKeyShare, secret) },
		"wrong space": func() error { return c.Verify(other, refRepo, h, refKeyShare, secret) },
		"wrong secret": func() error {
			return c.Verify(refSpace, refRepo, h, refKeyShare, bytes.Repeat([]byte{1}, 32))
		},
		"tampered data": func() error {
			tampered := c
			tampered.Data = bytes.Repeat([]byte{8}, 32)
			return tampered.Verify(refSpace, refRepo, h, refKeyShare, secret)
		},
		"unsupported version": func() error {
			v2 := c
			v2.Ver = 2
			return v2.Verify(refSpace, refRepo, h, refKeyShare, secret)
		},
	}
	for name, verify := range cases {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, verify(), ErrInvalidCommit)
		})
	}

	t.Run("another key share", func(t *testing.T) {
		require.ErrorIs(t,
			c.Verify(refSpace, refRepo, h, "did:key:zDnaOther", secret), ErrInvalidSpaceMAC)
	})
	t.Run("a record MAC is not a commit MAC", func(t *testing.T) {
		rh := http.Header{}
		recordReq := http.Header{}
		recordReq.Set(AcceptSignatureHeader, FormatMACRequest(RecordMACType, refKeyShare))
		req, err := ParseMACRequest(recordReq, RecordMACType)
		require.NoError(t, err)
		require.NoError(t, SetRecordMAC(rh, "at://x", cid.MustParse(refRecordCID), *req, secret))
		require.ErrorIs(t, c.Verify(refSpace, refRepo, rh, refKeyShare, secret), ErrInvalidSpaceMAC)
	})
}

func TestRecordMACBindsURIAndCID(t *testing.T) {
	secret := bytes.Repeat([]byte{9}, 32)
	uri := habitat_syntax.ConstructSpaceRecordURI(
		refSpace, refRepo, "com.example.record", "abc",
	).String()
	recordCID := cid.MustParse(refRecordCID)
	reqHeader := http.Header{}
	reqHeader.Set(AcceptSignatureHeader, FormatMACRequest(RecordMACType, refKeyShare))
	req, err := ParseMACRequest(reqHeader, RecordMACType)
	require.NoError(t, err)

	h := http.Header{}
	require.NoError(t, SetRecordMAC(h, uri, recordCID, *req, secret))
	require.Equal(t, `uri="`+uri+`", cid="`+refRecordCID+`"`, h.Get("Atproto-Space-Record"))
	require.NoError(t, VerifyRecordMAC(h, uri, recordCID, refKeyShare, secret))

	otherCID := cid.MustParse("bafyreihffx5a2e7k5uwrmmgofbvzujc5cmw5h4espouwuxt3liqoflx3ee")
	require.ErrorIs(t, VerifyRecordMAC(h, uri, otherCID, refKeyShare, secret), ErrInvalidCommit)
	require.ErrorIs(t, VerifyRecordMAC(h, uri+"2", recordCID, refKeyShare, secret), ErrInvalidCommit)
}

func TestCommitMatches(t *testing.T) {
	var lt LtHash
	lt.Add(RecordElement("com.example.post", "abc", refRecordCID))
	c := New(refSpace, refRepo, refRev, lt.Sum())
	require.True(t, c.Matches(&lt))
	require.False(t, c.Matches(&LtHash{}))
}

func TestParseMACRequestRejectsMalformed(t *testing.T) {
	req, err := ParseMACRequest(http.Header{}, CommitMACType)
	require.NoError(t, err)
	require.Nil(t, req, "an absent header is not an error")

	other := FormatMACRequest(RecordMACType, refKeyShare)
	h := http.Header{}
	h.Set(AcceptSignatureHeader, other)
	req, err = ParseMACRequest(h, CommitMACType)
	require.NoError(t, err)
	require.Nil(t, req, "requests for other tags are ignored")

	for name, value := range map[string]string{
		"not a dictionary": `"just a string"`,
		"wrong components": `mac=("authorization");keyid="did:key:zDnaX";tag="atproto-space-commit"`,
		"extra parameter":  refCommitRequest + `;created=1`,
		"wrong algorithm":  refCommitRequest + `;alg="ecdsa-p256-sha256"`,
		"missing keyid":    `mac=("atproto-space-commit";sf);tag="atproto-space-commit"`,
		"duplicate tag":    refCommitRequest + `, two=` + refCommitRequest[len("mac="):],
	} {
		t.Run(name, func(t *testing.T) {
			h := http.Header{}
			h.Set(AcceptSignatureHeader, value)
			_, err := ParseMACRequest(h, CommitMACType)
			require.ErrorIs(t, err, ErrInvalidSpaceMAC)
		})
	}
}

func TestVerifyCommitMACRejectsMissingHeaders(t *testing.T) {
	c := New(refSpace, refRepo, refRev, refData)
	secret := bytes.Repeat([]byte{9}, 32)
	h := http.Header{}
	require.NoError(t, SetCommitMAC(h, c, *refMACRequest(t), secret))
	for _, name := range []string{"Atproto-Space-Commit", "Signature-Input", "Signature"} {
		t.Run(name, func(t *testing.T) {
			trimmed := h.Clone()
			trimmed.Del(name)
			require.ErrorIs(t,
				c.Verify(refSpace, refRepo, trimmed, refKeyShare, secret), ErrInvalidSpaceMAC)
		})
	}
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
	require.ErrorIs(t, err, ErrInvalidSpaceMAC, "KEM keys are P-256")
	_, err = Decapsulate(refAuthorKEM(t), "not-a-did-key")
	require.ErrorIs(t, err, ErrInvalidSpaceMAC)
	_, err = Decapsulate(refAuthorKEM(t), k256Pub.DIDKey())
	require.ErrorIs(t, err, ErrInvalidSpaceMAC)
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
	require.ErrorIs(t, err, ErrInvalidSpaceMAC)
}
