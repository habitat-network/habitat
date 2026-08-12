package authn_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/did"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
)

func TestValidator(t *testing.T) {
	dir := identity.NewMockDirectory()
	hostKey, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	hostPubKey, err := hostKey.PublicKey()
	require.NoError(t, err)
	dir.Insert(*did.Web("alice").ATProtoSpaceKey(hostPubKey.Multibase()).Build())
	db := testutil.NewDB(t)
	oauth, err := oauthserver.NewOAuthServer(
		encrypt.TestKey,
		&org.LoginRouter{},
		nil,
		db,
		noop.Meter{},
		nil,
		"https://issuer.com",
		nil,
	)
	require.NoError(t, err)

	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })

	v := authn.NewValidator(
		oauth,
		nil,
		authn.NewSpaceCredentialAuthMethod(dir),
		authn.NewDelegationTokenAuthMethod(dir, fga, hostKey),
		fga,
	)

	t.Run("space credential", func(t *testing.T) {
		token, err := utils.SpaceCredential(
			hostKey,
			"#atproto_space",
			"at://did:web:alice/space/test.space.type/abc",
		)
		require.NoError(t, err)
		t.Run("matching space", func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/", http.NoBody)
			r.Header.Set("Authorization", "Bearer "+token)
			cred, ok := v.Request(
				authn.WithMethods(authn.ValidatorMethodSpaceCredential),
				authn.WithSpace(
					"at://did:web:alice/space/test.space.type/abc",
					fgastore.RelationSpaceReader,
				),
			).Validate(w, r)
			require.True(t, ok)
			require.NotNil(t, cred)
		})
		t.Run("other space", func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/", http.NoBody)
			r.Header.Set("Authorization", "Bearer "+token)
			cred, ok := v.Request(
				authn.WithMethods(
					authn.ValidatorMethodDelegationToken,
					authn.ValidatorMethodSpaceCredential,
				),
				authn.WithSpace(
					"at://did:web:alice/space/test.space.type/abcd",
					fgastore.RelationSpaceReader,
				),
			).Validate(w, r)
			require.False(t, ok)
			require.Nil(t, cred)
			require.Equal(t, http.StatusUnauthorized, w.Code)
		})
		t.Run("not supported method", func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/", http.NoBody)
			r.Header.Set("Authorization", "Bearer "+token)
			cred, ok := v.Request(
				authn.WithMethods(authn.ValidatorMethodOAuth),
				authn.WithSpace(
					"at://did:web:alice/space/test.space.type/abcd",
					fgastore.RelationSpaceReader,
				),
			).Validate(w, r)
			require.False(t, ok)
			require.Nil(t, cred)
			require.Equal(t, http.StatusUnauthorized, w.Code)
		})
	})
}
