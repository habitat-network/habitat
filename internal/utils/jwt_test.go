package utils_test

import (
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/habitat-network/habitat/internal/did"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/stretchr/testify/require"
)

func TestServiceAuthToken(t *testing.T) {
	privKey, _ := atcrypto.GeneratePrivateKeyK256()
	aud := "audience"
	lxm := new(syntax.NSID("test.xrpc.endpoint"))
	token, err := utils.ServiceAuthToken(
		privKey,
		"did:web:example.com",
		aud,
		lxm,
		new(time.Hour),
	)
	require.NoError(t, err)

	pubKey, _ := privKey.PublicKey()
	dir := identity.NewMockDirectory()
	dir.Insert(*did.Web("example.com").AtprotoKey(pubKey.Multibase()).Build())
	validator := auth.ServiceAuthValidator{
		Audience: aud,
		Dir:      dir,
	}
	did, err := validator.Validate(t.Context(), token, lxm)
	require.NoError(t, err)
	require.Equal(t, "did:web:example.com", did.String())
}

// TestJWTAlgMatchesKey checks every token pear signs names the algorithm of
// the key it was signed with: a verifier such as the atproto PDS checks the
// header's alg against the issuer's published key and refuses a mismatch.
func TestJWTAlgMatchesKey(t *testing.T) {
	space := habitat_syntax.ConstructSpaceURI("did:web:example.com", "com.example.space", "self")
	k256, err := atcrypto.GeneratePrivateKeyK256()
	require.NoError(t, err)
	p256, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	for _, tt := range []struct {
		name string
		key  atcrypto.PrivateKey
		alg  string
	}{
		{"K-256", k256, "ES256K"},
		{"P-256", p256, "ES256"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tokens := map[string]func() (string, error){
				"service auth": func() (string, error) {
					return utils.ServiceAuthToken(tt.key, "did:web:example.com", "aud", nil, nil)
				},
				"space credential": func() (string, error) {
					return utils.SpaceCredential(tt.key, space)
				},
				"delegation token": func() (string, error) {
					return utils.DelegationToken(tt.key, "did:web:example.com", "#atproto", space)
				},
			}
			for kind, mint := range tokens {
				token, err := mint()
				require.NoError(t, err, kind)
				parsed, _, err := jwt.NewParser().ParseUnverified(token, jwt.MapClaims{})
				require.NoError(t, err, kind)
				require.Equal(t, tt.alg, parsed.Header["alg"], kind)
			}
		})
	}
}
