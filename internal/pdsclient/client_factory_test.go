package pdsclient

import (
	"testing"
	"time"

	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/stretchr/testify/require"
)

func TestClientFactory_ReusesDpopClientForSameUser(t *testing.T) {
	clientFactory, err := NewHttpClientFactory(
		testPdsCredStore(t, jwt.Claims{
			Expiry: jwt.NewNumericDate(time.Now().Add(10 * time.Minute)),
		}),
		testOAuthClient(t),
		NewDummyDirectory("https://pds.example.com"),
	)
	require.NoError(t, err)

	did := testIdentity("https://pds.example.com").DID

	client1, err := clientFactory.NewClient(t.Context(), did)
	require.NoError(t, err)

	client2, err := clientFactory.NewClient(t.Context(), did)
	require.NoError(t, err)

	c1, ok := client1.(*authedDpopHttpClient)
	require.True(t, ok)
	c2, ok := client2.(*authedDpopHttpClient)
	require.True(t, ok)
	require.Same(t, c1, c2, "expected the same dpop client to be reused for the same DID")
}
