package authn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

func TestStubAuthn(t *testing.T) {
	const testDID = syntax.DID("did:web:stub.test")

	t.Run("valid stub", func(t *testing.T) {
		stub := NewStubAuthnForTest(testDID)

		r := httptest.NewRequest("GET", "/", nil)
		require.True(t, stub.CanHandle(r))

		w := httptest.NewRecorder()
		did, ok := stub.Validate(w, r)
		require.True(t, ok)
		require.Equal(t, testDID, did)

		did, ok, err := stub.ValidateRaw(context.Background(), "any-token")
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, testDID, did)
	})

	t.Run("failed stub", func(t *testing.T) {
		stub := NewStubAuthnFailedForTest()

		r := httptest.NewRequest("GET", "/", nil)
		require.True(t, stub.CanHandle(r))

		w := httptest.NewRecorder()
		did, ok := stub.Validate(w, r)
		require.False(t, ok)
		require.Empty(t, did)
		require.Equal(t, http.StatusUnauthorized, w.Code)

		did, ok, err := stub.ValidateRaw(context.Background(), "any-token")
		require.NoError(t, err)
		require.False(t, ok)
		require.Empty(t, did)
	})
}

func TestStubAuthn_Validate(t *testing.T) {
	t.Run("writes 401 on failed auth", func(t *testing.T) {
		stub := NewStubAuthnFailedForTest()
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		did, ok := stub.Validate(w, r)
		require.False(t, ok)
		require.Empty(t, did)
		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.Contains(t, w.Body.String(), http.StatusText(http.StatusUnauthorized))
	})
}
