package authn

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

func TestStubAuthn(t *testing.T) {
	const testDID = syntax.DID("did:web:stub.test")
	stub := NewStubAuthnForTest(testDID)

	t.Run("CanHandle always returns true", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		require.True(t, stub.CanHandle(r))
	})

	t.Run("Validate returns configured DID", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		did, ok := stub.Validate(w, r)
		require.True(t, ok)
		require.Equal(t, testDID, did)
	})

	t.Run("ValidateRaw returns configured DID", func(t *testing.T) {
		did, ok, err := stub.ValidateRaw(context.Background(), "any-token")
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, testDID, did)
	})
}
