package utils_test

import (
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/did"
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
