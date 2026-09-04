package authn

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/habitat-network/habitat/internal/did"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/stretchr/testify/require"
)

var testLxm = syntax.NSID("io.example.test")

const (
	testServiceHost     = "aud.example.com"
	testHabitatEndpoint = "https://habitat.example.com"
)

func newTestRequest(t *testing.T, token string) *http.Request {
	t.Helper()
	r := httptest.NewRequest("GET", "/xrpc/io.example.test", http.NoBody)
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

func TestServiceAuth(t *testing.T) {
	// Shared setup: a MockDirectory with two identities -- a user with an
	// atproto repo signing key (the token issuer), and a service exposing a
	// #habitat service endpoint (a possible token audience) -- plus a legacy
	// DID the method is configured with directly (no directory lookup).
	dir := identity.NewMockDirectory()

	userDID := syntax.DID("did:plc:test")
	userKey, err := atcrypto.GeneratePrivateKeyK256()
	require.NoError(t, err)
	userPub, err := userKey.PublicKey()
	require.NoError(t, err)
	dir.Insert(*did.New(userDID).AtprotoKey(userPub.Multibase()).Build())

	serviceDID := syntax.DID("did:web:" + testServiceHost)
	dir.Insert(*did.Web(testServiceHost).Habitat(testHabitatEndpoint).Build())

	legacyDID := syntax.DID("did:plc:legacy")

	t.Run("Validate", func(t *testing.T) {
		tests := []struct {
			name            string
			aud             string
			serviceEndpoint string
			wantOK          bool
		}{
			{
				name:   "legacy bare DID audience matching configured legacy DID",
				aud:    legacyDID.String(),
				wantOK: true,
			},
			{
				name:   "legacy bare DID audience not matching configured legacy DID",
				aud:    "did:plc:someone-else",
				wantOK: false,
			},
			{
				name:            "aud with service fragment resolves via serviceEndpoint lookup",
				aud:             serviceDID.String() + "#habitat",
				serviceEndpoint: testHabitatEndpoint,
				wantOK:          true,
			},
			{
				name:            "aud with service fragment whose endpoint does not match",
				aud:             serviceDID.String() + "#habitat",
				serviceEndpoint: "https://not-the-habitat.com",
				wantOK:          false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				token, err := utils.ServiceAuthToken(userKey, userDID, tt.aud, &testLxm, nil)
				require.NoError(t, err)

				serviceAuth := NewServiceAuthMethod(
					org.NewEveryoneOrg("everyone.example.com"),
					dir,
					legacyDID,
					tt.serviceEndpoint,
				)
				w := httptest.NewRecorder()
				credInfo, ok := serviceAuth.Validate(w, newTestRequest(t, token))

				require.Equal(t, tt.wantOK, ok)
				if tt.wantOK {
					require.Equal(t, userDID, credInfo.Subject)
				} else {
					require.Nil(t, credInfo)
					require.Equal(t, http.StatusUnauthorized, w.Code)
				}
			})
		}
	})

	t.Run("Validate_EmptyAudience", func(t *testing.T) {
		serviceAuth := NewServiceAuthMethod(
			org.NewEveryoneOrg("everyone.example.com"),
			dir,
			legacyDID,
			testHabitatEndpoint,
		)
		// Built without utils.ServiceAuthToken (which always writes an "aud"
		// claim): a JWS with no "aud" claim at all. Signature verification
		// never happens for this case (Validate rejects the empty aud claim
		// before validating the signature), so any signer works.
		token, err := jwt.NewWithClaims(jwt.GetSigningMethod("ES256K"), jwt.MapClaims{
			"iss": userDID.String(),
			"iat": jwt.NewNumericDate(time.Now()),
			"exp": jwt.NewNumericDate(time.Now().Add(time.Hour)),
		}).SignedString(userKey)
		require.NoError(t, err)
		w := httptest.NewRecorder()

		credInfo, ok := serviceAuth.Validate(w, newTestRequest(t, token))

		require.False(t, ok)
		require.Nil(t, credInfo)
		require.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("Validate_InvalidToken", func(t *testing.T) {
		serviceAuth := NewServiceAuthMethod(
			org.NewEveryoneOrg("everyone.example.com"),
			dir,
			legacyDID,
			testHabitatEndpoint,
		)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/xrpc/lxm", http.NoBody)
		r.Header.Set("Authorization", "Bearer invalid")
		_, ok := serviceAuth.Validate(w, r)
		require.False(t, ok)
	})

	t.Run("CanHandle", func(t *testing.T) {
		token, err := utils.ServiceAuthToken(userKey, userDID, testHabitatEndpoint, &testLxm, nil)
		require.NoError(t, err)
		serviceAuth := NewServiceAuthMethod(
			org.NewEveryoneOrg("everyone.example.com"),
			dir,
			legacyDID,
			testHabitatEndpoint,
		)
		r := httptest.NewRequest("GET", "/", http.NoBody)
		r.Header.Set("Authorization", "Bearer "+token)
		require.True(t, serviceAuth.CanHandle(r))
	})
}
