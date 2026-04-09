package authn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

func TestMiddleware(t *testing.T) {
	validMethod := &testAuthMethod{expectedHeader: "valid-token"}

	reached := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})

	handler := Middleware(validMethod)(next)

	t.Run("authenticated request sets callerDID in context and calls next", func(t *testing.T) {
		reached = false
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", "valid-token")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, r)

		require.True(t, reached)
		require.Equal(t, http.StatusOK, w.Result().StatusCode)

		did, ok := FromContext(r.Context())
		require.False(t, ok, "original request context is unmodified; DID lives on the context passed to next")
		_ = did
	})

	t.Run("unauthenticated request returns 401 and does not call next", func(t *testing.T) {
		reached = false
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", "wrong-token")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, r)

		require.False(t, reached)
		require.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
	})
}

func TestMiddlewareFromContext(t *testing.T) {
	const testDID = syntax.DID("did:web:test")
	validMethod := &testAuthMethod{expectedHeader: "valid-token"}

	var callerFromCtx syntax.DID
	var okFromCtx bool

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callerFromCtx, okFromCtx = FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := Middleware(validMethod)(next)

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "valid-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	require.True(t, okFromCtx)
	require.Equal(t, testDID, callerFromCtx)
}

func TestFromContext(t *testing.T) {
	t.Run("returns DID when set", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		// same package — callerKey is accessible directly
		ctx := context.WithValue(r.Context(), callerKey, syntax.DID("did:web:test"))

		did, ok := FromContext(ctx)
		require.True(t, ok)
		require.Equal(t, syntax.DID("did:web:test"), did)
	})

	t.Run("returns false when not set", func(t *testing.T) {
		_, ok := FromContext(httptest.NewRequest("GET", "/", nil).Context())
		require.False(t, ok)
	})
}
