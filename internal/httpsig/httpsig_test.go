package httpsig_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/httpsig"
)

func newRequest() *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.Header.Set("Authorization", "Atproto-Space token")
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
		require.False(t, httpsig.HasSignature(r))
		require.NoError(t, httpsig.Sign(r, key, comps...))
		require.True(t, httpsig.HasSignature(r))
		got, err := httpsig.Verify(r, comps...)
		require.NoError(t, err)
		require.Equal(t, pub.DIDKey(), got)
	})

	t.Run("tampered audience", func(t *testing.T) {
		r := newRequest()
		require.NoError(t, httpsig.Sign(r, key, comps...))
		r.Header.Set(httpsig.AudienceHeader, "did:web:evil.example")
		_, err := httpsig.Verify(r, comps...)
		require.ErrorIs(t, err, httpsig.ErrInvalidSignature)
	})

	t.Run("wrong components", func(t *testing.T) {
		r := newRequest()
		require.NoError(t, httpsig.Sign(r, key, "authorization"))
		_, err := httpsig.Verify(r, comps...)
		require.ErrorIs(t, err, httpsig.ErrInvalidSignature)
	})

	t.Run("missing signature", func(t *testing.T) {
		_, err := httpsig.Verify(newRequest(), comps...)
		require.ErrorIs(t, err, httpsig.ErrNoSignature)
	})

	t.Run("k256 key rejected", func(t *testing.T) {
		k256, err := atcrypto.GeneratePrivateKeyK256()
		require.NoError(t, err)
		require.Error(t, httpsig.Sign(newRequest(), k256, comps...))
	})
}

// Headers produced by @atproto/space's createSpaceSigHeaders, the reference
// client: unpadded base64, and no keyid on a credential-use signature (the
// key is the credential's cnf.kid).
const (
	refKey      = "did:key:zDnaejNzg9pNn2yebVfDMg9vUT3jUtZrGSsg6ym4jdRrcVvb4"
	refExchange = `atproto-space=:jAI1IlrX8im5H1Tvb7BZHIrR5XE7P1O6/s/LjD//pZRq8G4NPyF+Lbgx5/rZ24HZAvVp/I56yfRSIGj4pA02bA:`
	refUse      = `atproto-space=:C5HzzP2/JNfzALGEhg/PKHfvZZtT3qL53ayMJ9AeJw1CaKN6eqyS80MQlJ6SWwZ8ZtBXBgaj0Jf2ewscUSOshg:`
)

func TestVerifyReferenceClient(t *testing.T) {
	exchange := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/", http.NoBody)
		r.Header.Set("Authorization", "Bearer deleg.token.x")
		r.Header.Set("Signature-Input", `atproto-space=("authorization");keyid="`+refKey+`"`)
		r.Header.Set("Signature", refExchange)
		return r
	}
	use := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		r.Header.Set("Authorization", "Atproto-Space cred.token.y")
		r.Header.Set(httpsig.AudienceHeader, "did:plc:repoowner")
		r.Header.Set("Signature-Input", `atproto-space=("authorization" "atproto-space-audience")`)
		r.Header.Set("Signature", refUse)
		return r
	}
	comps := []string{"authorization", "atproto-space-audience"}

	t.Run("exchange", func(t *testing.T) {
		got, err := httpsig.Verify(exchange(), "authorization")
		require.NoError(t, err)
		require.Equal(t, refKey, got)
	})

	t.Run("credential use without keyid", func(t *testing.T) {
		require.NoError(t, httpsig.VerifyBound(use(), refKey, comps...))
	})

	t.Run("credential use needs a keyid to name the signer", func(t *testing.T) {
		_, err := httpsig.Verify(use(), comps...)
		require.ErrorIs(t, err, httpsig.ErrInvalidSignature)
	})

	t.Run("credential use by another key", func(t *testing.T) {
		other, err := atcrypto.GeneratePrivateKeyP256()
		require.NoError(t, err)
		pub, err := other.PublicKey()
		require.NoError(t, err)
		err = httpsig.VerifyBound(use(), pub.DIDKey(), comps...)
		require.ErrorIs(t, err, httpsig.ErrInvalidSignature)
	})

	t.Run("keyid that disagrees with the bound key", func(t *testing.T) {
		r := exchange()
		err := httpsig.VerifyBound(r, "did:key:zDnaOther", "authorization")
		require.ErrorIs(t, err, httpsig.ErrInvalidSignature)
	})
}

func TestVerifyBoundRoundTrip(t *testing.T) {
	key, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	pub, err := key.PublicKey()
	require.NoError(t, err)
	comps := []string{"authorization", "atproto-space-audience"}

	r := newRequest()
	require.NoError(t, httpsig.Sign(r, key, comps...))
	require.NoError(t, httpsig.VerifyBound(r, pub.DIDKey(), comps...))
}
