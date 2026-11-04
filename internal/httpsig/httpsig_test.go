package httpsig_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/httpsig"
)

func newRequest() *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.Header.Set("Authorization", "Atproto-Space-Credential token")
	r.Header.Set(httpsig.AudienceHeader, "did:web:host.example")
	return r
}

func TestSignVerify(t *testing.T) {
	key, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	pub, err := key.PublicKey()
	require.NoError(t, err)
	comps := []string{"authorization", "atproto-space-audience"}

	t.Run("round trip", func(t *testing.T) {
		r := newRequest()
		require.NoError(t, httpsig.Sign(r, key, comps...))
		require.Equal(
			t,
			`atproto-space-credential=("authorization" "atproto-space-audience");tag="atproto-space-credential"`,
			r.Header.Get("Signature-Input"),
		)
		require.NoError(t, httpsig.VerifyBound(r, pub.DIDKey(), comps...))
	})

	t.Run("tampered audience", func(t *testing.T) {
		r := newRequest()
		require.NoError(t, httpsig.Sign(r, key, comps...))
		r.Header.Set(httpsig.AudienceHeader, "did:web:evil.example")
		err := httpsig.VerifyBound(r, pub.DIDKey(), comps...)
		require.ErrorIs(t, err, httpsig.ErrInvalidSignature)
	})

	t.Run("wrong components", func(t *testing.T) {
		r := newRequest()
		require.NoError(t, httpsig.Sign(r, key, "authorization"))
		err := httpsig.VerifyBound(r, pub.DIDKey(), comps...)
		require.ErrorIs(t, err, httpsig.ErrInvalidSignature)
	})

	t.Run("missing signature", func(t *testing.T) {
		err := httpsig.VerifyBound(newRequest(), pub.DIDKey(), comps...)
		require.ErrorIs(t, err, httpsig.ErrNoSignature)
	})

	t.Run("other key", func(t *testing.T) {
		other, err := atcrypto.GeneratePrivateKeyP256()
		require.NoError(t, err)
		otherPub, err := other.PublicKey()
		require.NoError(t, err)
		r := newRequest()
		require.NoError(t, httpsig.Sign(r, key, comps...))
		err = httpsig.VerifyBound(r, otherPub.DIDKey(), comps...)
		require.ErrorIs(t, err, httpsig.ErrInvalidSignature)
	})

	t.Run("k256 key rejected", func(t *testing.T) {
		k256, err := atcrypto.GeneratePrivateKeyK256()
		require.NoError(t, err)
		require.Error(t, httpsig.Sign(newRequest(), k256, comps...))
	})
}

// Headers produced by @atproto/space@0.0.0-spaces-alpha-20261029171913's
// createSpaceSigHeaders, the reference client: padded base64, a tag, and no
// keyid (the key is the credential's cnf.kid).
const (
	refKey   = "did:key:zDnaeaU83PGTU2SxCaKFboswW4VGLLq7rMSaqsnYcq9dYbMP8"
	refInput = `atproto-space-credential=("authorization" "atproto-space-audience");tag="atproto-space-credential"`
	refSig   = `atproto-space-credential=:7HBAwkylzezrImiRmrJ4CU3fygMclaoqJ7M+NxXoH20pvXQRx5M4S3r9YVqsvPtg/590ZrVast5L0XWUoKNcXw==:`
)

func TestVerifyReferenceClient(t *testing.T) {
	comps := []string{"authorization", "atproto-space-audience"}
	use := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		r.Header.Set("Authorization", "Atproto-Space-Credential cred.token.y")
		r.Header.Set(httpsig.AudienceHeader, "did:plc:repoowner")
		r.Header.Set("Signature-Input", refInput)
		r.Header.Set("Signature", refSig)
		return r
	}

	t.Run("accepted", func(t *testing.T) {
		require.NoError(t, httpsig.VerifyBound(use(), refKey, comps...))
	})

	t.Run("unpadded base64", func(t *testing.T) {
		r := use()
		r.Header.Set("Signature", strings.Replace(refSig, "==:", ":", 1))
		require.NoError(t, httpsig.VerifyBound(r, refKey, comps...))
	})

	t.Run("alongside other signatures", func(t *testing.T) {
		r := use()
		r.Header.Set("Signature-Input", `sig1=("@method");keyid="x", `+refInput)
		r.Header.Set("Signature", `sig1=:AAAA:, `+refSig)
		require.NoError(t, httpsig.VerifyBound(r, refKey, comps...))
	})

	t.Run("matching keyid", func(t *testing.T) {
		// keyid is part of the signed parameters, so adding it breaks this
		// signature; it's checked against the bound key before that.
		r := use()
		r.Header.Set("Signature-Input", refInput+`;keyid="did:key:zDnaOther"`)
		err := httpsig.VerifyBound(r, refKey, comps...)
		require.ErrorContains(t, err, "keyid does not match")
	})

	t.Run("missing tag", func(t *testing.T) {
		r := use()
		r.Header.Set(
			"Signature-Input",
			strings.TrimSuffix(refInput, `;tag="atproto-space-credential"`),
		)
		err := httpsig.VerifyBound(r, refKey, comps...)
		require.ErrorContains(t, err, "tag")
	})

	t.Run("wrong tag", func(t *testing.T) {
		r := use()
		r.Header.Set(
			"Signature-Input",
			strings.Replace(refInput, `tag="atproto-space-credential"`, `tag="other"`, 1),
		)
		err := httpsig.VerifyBound(r, refKey, comps...)
		require.ErrorContains(t, err, "tag")
	})

	t.Run("wrong alg", func(t *testing.T) {
		r := use()
		r.Header.Set("Signature-Input", refInput+`;alg="rsa-pss-sha512"`)
		err := httpsig.VerifyBound(r, refKey, comps...)
		require.ErrorContains(t, err, "algorithm")
	})

	t.Run("old label", func(t *testing.T) {
		r := use()
		r.Header.Set("Signature-Input", `atproto-space=("authorization" "atproto-space-audience")`)
		r.Header.Set(
			"Signature",
			strings.Replace(refSig, "atproto-space-credential=", "atproto-space=", 1),
		)
		err := httpsig.VerifyBound(r, refKey, comps...)
		require.ErrorIs(t, err, httpsig.ErrNoSignature)
	})
}

func TestIsP256DIDKey(t *testing.T) {
	require.True(t, httpsig.IsP256DIDKey(refKey))
	k256, err := atcrypto.GeneratePrivateKeyK256()
	require.NoError(t, err)
	pub, err := k256.PublicKey()
	require.NoError(t, err)
	require.False(t, httpsig.IsP256DIDKey(pub.DIDKey()))
	require.False(t, httpsig.IsP256DIDKey("did:key:zDnaNotAKey"))
	require.False(t, httpsig.IsP256DIDKey(""))
}
