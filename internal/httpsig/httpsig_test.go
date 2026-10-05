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
