package authmethods

import (
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/go-jose/go-jose/v3"
	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/stretchr/testify/require"
)

func TestServiceAuth_Validate(t *testing.T) {
	directory := oauthclient.NewDummyDirectory("https://pds.com")
	signer, err := jose.NewSigner(jose.SigningKey{
		Algorithm: "ES256K",
		Key:       atcryptoSigner{directory.PrivateKey},
	}, nil)
	require.NoError(t, err, "failed to create signer")
	token, err := jwt.Signed(signer).Claims(serviceJwtPayload{
		Iss: "did:plc:test",
		Aud: "https://pds.com",
		Exp: 0,
		Lxm: "lxm",
	}).CompactSerialize()
	require.NoError(t, err, "failed to create token")
	serviceAuth := NewServiceAuthMethod(directory)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+token)

	resultDid, ok := serviceAuth.Validate(w, r)

	require.True(t, ok)
	require.Equal(t, syntax.DID("did:plc:test"), resultDid)
}

type atcryptoSigner struct {
	atcrypto.PrivateKey
}

var _ jose.OpaqueSigner = (*atcryptoSigner)(nil)

// Algs implements [jose.OpaqueSigner].
func (a atcryptoSigner) Algs() []jose.SignatureAlgorithm {
	switch a.PrivateKey.(type) {
	case *atcrypto.PrivateKeyK256:
		return []jose.SignatureAlgorithm{"ES256K"}
	case *atcrypto.PrivateKeyP256:
		return []jose.SignatureAlgorithm{"ES256"}
	}
	return nil
}

// Public implements [jose.OpaqueSigner].
func (a atcryptoSigner) Public() *jose.JSONWebKey {
	return nil
}

// SignPayload implements [jose.OpaqueSigner].
func (a atcryptoSigner) SignPayload(payload []byte, alg jose.SignatureAlgorithm) ([]byte, error) {
	return a.PrivateKey.HashAndSign(payload)
}
