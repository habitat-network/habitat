package authn_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/golang-jwt/jwt/v5"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/did"
	"github.com/habitat-network/habitat/internal/httpsig"
	keyval_testutil "github.com/habitat-network/habitat/internal/keyval/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/stretchr/testify/require"
)

func newAuthenticatedRequest(token string) *http.Request {
	r := httptest.NewRequest("GET", "/", http.NoBody)
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

func newRevocations(t *testing.T) *authn.CredentialRevocations {
	t.Helper()
	return authn.NewCredentialRevocations(keyval_testutil.New(t).Namespace("revoked"))
}

// presentCredential builds a request to target presenting token as a space
// credential, signed by key for audience as the spaces proposal requires.
func presentCredential(
	t *testing.T,
	target, token string,
	key atcrypto.PrivateKey,
	audience string,
) *http.Request {
	t.Helper()
	r := httptest.NewRequest("GET", target, http.NoBody)
	r.Header.Set("Authorization", "Atproto-Space-Credential "+token)
	r.Header.Set(httpsig.AudienceHeader, audience)
	require.NoError(t, httpsig.Sign(r, key, "authorization", "atproto-space-audience"))
	return r
}

// newBindingKey returns a P-256 key to bind a credential to, and its did:key.
func newBindingKey(t *testing.T) (atcrypto.PrivateKey, string) {
	t.Helper()
	key, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	pub, err := key.PublicKey()
	require.NoError(t, err)
	return key, pub.DIDKey()
}

func TestSpaceCredentialAuthMethod(t *testing.T) {
	hostKey, _ := atcrypto.GeneratePrivateKeyK256()
	hostPubKey, _ := hostKey.PublicKey()
	dir := identity.NewMockDirectory()
	dir.Insert(
		*did.Web("pear.com").AtprotoKey(hostPubKey.Multibase()).Build(),
	)
	method := authn.NewSpaceCredentialAuthMethod(dir, newRevocations(t))

	t.Run("rejects the legacy #atproto_space kid", func(t *testing.T) {
		dir := identity.NewMockDirectory()
		dir.Insert(*did.Web("pear.com").
			AtprotoKey(hostPubKey.Multibase()).
			VerificationMethod("atproto_space", "Multikey", hostPubKey.Multibase()).
			Build())
		token, err := new(jwt.Token{
			Method: jwt.GetSigningMethod("ES256K"),
			Claims: jwt.MapClaims{
				"iss": "did:web:pear.com",
				"sub": "at://did:web:pear.com/space/com.test.space/abc",
				"iat": jwt.NewNumericDate(time.Now()),
				"exp": jwt.NewNumericDate(time.Now().Add(time.Minute)),
			},
			Header: map[string]any{
				"typ": "atproto-space-credential+jwt", "kid": "#atproto_space", "alg": "ES256K",
			},
		}).SignedString(hostKey)
		require.NoError(t, err)
		credInfo, ok := authn.NewSpaceCredentialAuthMethod(dir, newRevocations(t)).
			Validate(httptest.NewRecorder(), newAuthenticatedRequest(token))
		require.False(t, ok)
		require.Nil(t, credInfo)
	})

	t.Run("atproto", func(t *testing.T) {
		key, kid := newBindingKey(t)
		token, err := utils.SpaceCredential(
			hostKey,
			"at://did:web:pear.com/space/com.test.space/abc",
			utils.WithConfirmationKey(kid),
		)
		require.NoError(t, err)
		r := presentCredential(t, "/", token, key, "did:web:pear.com")
		require.True(t, method.CanHandle(r))
		credInfo, ok := method.Validate(httptest.NewRecorder(), r)
		require.True(t, ok)
		require.Equal(t, credInfo.Space.String(), "at://did:web:pear.com/space/com.test.space/abc")
		require.Empty(t, credInfo.Subject)
		require.Equal(t, authn.ValidatorMethodSpaceCredential, credInfo.Method)
	})

	t.Run("no token", func(t *testing.T) {
		_, ok := method.Validate(
			httptest.NewRecorder(),
			httptest.NewRequest("GET", "/", http.NoBody),
		)
		require.False(t, ok)
	})

	t.Run("invalid signature", func(t *testing.T) {
		otherKey, _ := atcrypto.GeneratePrivateKeyK256()
		token, _ := utils.SpaceCredential(
			otherKey,
			"at://did:web:pear.com/space/com.test.space/abc",
		)
		r := newAuthenticatedRequest(token)
		require.True(t, method.CanHandle(r))
		credInfo, ok := method.Validate(httptest.NewRecorder(), r)
		require.False(t, ok)
		require.Nil(t, credInfo)
	})

	t.Run("issuer mismatch", func(t *testing.T) {
		token, _ := new(jwt.Token{
			Header: map[string]any{
				"typ": "atproto-space-credential+jwt",
				"alg": "ES256K",
				"kid": "#atproto",
			},
			Claims: jwt.MapClaims{
				"iss": "did:web:pear.com",
				"sub": "at://did:web:other.example.com/space/test.space.type/abc",
				"exp": jwt.NewNumericDate(time.Now().Add(time.Hour)),
			},
			Method: jwt.GetSigningMethod("ES256K"),
		}).SignedString(hostKey)
		r := newAuthenticatedRequest(token)
		require.True(t, method.CanHandle(r))
		credInfo, ok := method.Validate(httptest.NewRecorder(), r)
		require.False(t, ok)
		require.Nil(t, credInfo)
	})

	const space = "at://did:web:pear.com/space/com.test.space/abc"

	t.Run("expires in 10 minutes with a random jti", func(t *testing.T) {
		claimsOf := func(token string) jwt.MapClaims {
			claims := jwt.MapClaims{}
			_, _, err := jwt.NewParser().ParseUnverified(token, claims)
			require.NoError(t, err)
			return claims
		}
		a, err := utils.SpaceCredential(hostKey, space)
		require.NoError(t, err)
		b, err := utils.SpaceCredential(hostKey, space)
		require.NoError(t, err)
		ca, cb := claimsOf(a), claimsOf(b)
		require.InDelta(t, ca["iat"], ca["exp"].(float64)-600, 0)
		require.NotEmpty(t, ca["jti"])
		require.NotEqual(t, ca["jti"], cb["jti"])
	})

	t.Run("still accepts an older one hour credential", func(t *testing.T) {
		key, kid := newBindingKey(t)
		now := time.Now()
		token, err := new(jwt.Token{
			Method: jwt.GetSigningMethod("ES256K"),
			Claims: jwt.MapClaims{
				"iss": "did:web:pear.com",
				"sub": space,
				"iat": jwt.NewNumericDate(now),
				"exp": jwt.NewNumericDate(now.Add(time.Hour)),
				"jti": "legacy",
				"cnf": map[string]any{"kid": kid},
			},
			Header: map[string]any{
				"typ": "atproto-space-credential+jwt", "kid": "#atproto", "alg": "ES256K",
			},
		}).SignedString(hostKey)
		require.NoError(t, err)
		_, ok := method.Validate(
			httptest.NewRecorder(), presentCredential(t, "/", token, key, "did:web:pear.com"),
		)
		require.True(t, ok)
	})
}

func TestSpaceCredentialAuthMethod_KeyBinding(t *testing.T) {
	hostKey, _ := atcrypto.GeneratePrivateKeyK256()
	hostPubKey, _ := hostKey.PublicKey()
	dir := identity.NewMockDirectory()
	dir.Insert(
		*did.Web("pear.com").AtprotoKey(hostPubKey.Multibase()).Build(),
	)
	method := authn.NewSpaceCredentialAuthMethod(dir, newRevocations(t))
	space := habitat_syntax.SpaceURI("at://did:web:pear.com/space/com.test.space/abc")

	syncerKey, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	syncerPub, err := syncerKey.PublicKey()
	require.NoError(t, err)
	bound, err := utils.SpaceCredential(
		hostKey, space, utils.WithConfirmationKey(syncerPub.DIDKey()),
	)
	require.NoError(t, err)

	signed := func(key atcrypto.PrivateKey, audience string) *http.Request {
		r := httptest.NewRequest("GET", "/", http.NoBody)
		r.Header.Set("Authorization", "atproto-space-credential "+bound)
		r.Header.Set(httpsig.AudienceHeader, audience)
		require.NoError(t, httpsig.Sign(r, key, "authorization", "atproto-space-audience"))
		return r
	}

	t.Run("signed by bound key", func(t *testing.T) {
		r := signed(syncerKey, "did:web:pear.com")
		require.True(t, method.CanHandle(r))
		info, ok := method.Validate(httptest.NewRecorder(), r)
		require.True(t, ok)
		require.Equal(t, space, info.Space)
		require.Equal(t, "did:web:pear.com", info.Audience)
	})

	t.Run("unsigned bound credential rejected", func(t *testing.T) {
		_, ok := method.Validate(httptest.NewRecorder(), newAuthenticatedRequest(bound))
		require.False(t, ok)
	})

	t.Run("unbound credential rejected", func(t *testing.T) {
		unbound, err := utils.SpaceCredential(hostKey, space)
		require.NoError(t, err)
		r := presentCredential(t, "/", unbound, syncerKey, "did:web:pear.com")
		_, ok := method.Validate(httptest.NewRecorder(), r)
		require.False(t, ok)
	})

	t.Run("audience is the repo for repo methods", func(t *testing.T) {
		const target = "/xrpc/com.atproto.space.getRepo?repo=did:plc:alice"
		info, ok := method.Validate(
			httptest.NewRecorder(),
			presentCredential(t, target, bound, syncerKey, "did:plc:alice"),
		)
		require.True(t, ok)
		require.Equal(t, "did:plc:alice", info.Audience)
	})

	t.Run("signed for another audience rejected", func(t *testing.T) {
		const target = "/xrpc/com.atproto.space.getRepo?repo=did:plc:alice"
		for _, audience := range []string{"did:web:pear.com", "did:plc:bob"} {
			w := httptest.NewRecorder()
			_, ok := method.Validate(w, presentCredential(t, target, bound, syncerKey, audience))
			require.False(t, ok, audience)
			require.Equal(t, http.StatusUnauthorized, w.Code, audience)
		}
		// A host method addresses the space authority.
		_, ok := method.Validate(
			httptest.NewRecorder(),
			presentCredential(t, "/xrpc/com.atproto.space.listRepos", bound, syncerKey, "did:plc:alice"),
		)
		require.False(t, ok)
	})

	t.Run("revoked credential rejected", func(t *testing.T) {
		revocations := newRevocations(t)
		method := authn.NewSpaceCredentialAuthMethod(dir, revocations)
		claims := jwt.MapClaims{}
		_, _, err := jwt.NewParser().ParseUnverified(bound, claims)
		require.NoError(t, err)
		require.NoError(t, revocations.Revoke(t.Context(), space, []string{claims["jti"].(string)}))

		w := httptest.NewRecorder()
		_, ok := method.Validate(w, signed(syncerKey, "did:web:pear.com"))
		require.False(t, ok)
		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.Contains(t, w.Body.String(), "CredentialRevoked")

		// Revocation is per space: the same jti elsewhere is unaffected.
		other, err := revocations.IsRevoked(
			t.Context(), "at://did:web:pear.com/space/com.test.space/other", claims["jti"].(string),
		)
		require.NoError(t, err)
		require.False(t, other)
	})

	t.Run("signed by other key rejected", func(t *testing.T) {
		other, err := atcrypto.GeneratePrivateKeyP256()
		require.NoError(t, err)
		_, ok := method.Validate(httptest.NewRecorder(), signed(other, "did:web:pear.com"))
		require.False(t, ok)
	})
}
