package authn

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

func TestValidate(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	r.Header.Set("Authorization", "foo")
	did, ok := Validate(
		w,
		r,
		&testAuthMethod{expectedHeader: "foo"},
		&testAuthMethod{expectedHeader: "bar"},
	)
	require.True(t, ok)
	require.Equal(t, syntax.DID("did:web:test"), did)

	w = httptest.NewRecorder()
	r.Header.Set("Authorization", "bar")
	_, ok = Validate(
		w,
		r,
		&testAuthMethod{expectedHeader: "foo"},
		&testAuthMethod{expectedHeader: "bar", fail: true},
	)
	require.False(t, ok)
	require.Equal(t, w.Result().StatusCode, http.StatusUnauthorized)

	w = httptest.NewRecorder()
	r.Header.Set("Authorization", "foo")
	_, ok = Validate(
		w,
		r,
		&testAuthMethod{expectedHeader: "bar"},
	)
	require.False(t, ok)
	require.Equal(t, w.Result().StatusCode, http.StatusUnauthorized)
}

type testAuthMethod struct {
	expectedHeader string
	fail           bool
}

// CanHandle implements [Method].
func (t *testAuthMethod) CanHandle(r *http.Request) bool {
	return r.Header.Get("Authorization") == t.expectedHeader
}

// Validate implements [Method].
func (t *testAuthMethod) Validate(
	w http.ResponseWriter,
	r *http.Request,
	scopes ...string,
) (syntax.DID, bool) {
	if t.fail {
		w.WriteHeader(http.StatusUnauthorized)
		return "", false
	}
	return syntax.DID("did:web:test"), true
}

// ValidateRaw implements [Method].
func (t *testAuthMethod) ValidateRaw(ctx context.Context, token string, scopes ...string) (syntax.DID, bool, error) {
	if token != t.expectedHeader {
		return "", false, fmt.Errorf("unexpected token")
	}
	if t.fail {
		return "", false, fmt.Errorf("auth failed")
	}
	return syntax.DID("did:web:test"), true, nil
}

func TestValidateRaw(t *testing.T) {
	ctx := context.Background()

	// First method handles it successfully.
	did, ok, err := ValidateRaw(
		ctx,
		"foo",
		&testAuthMethod{expectedHeader: "foo"},
		&testAuthMethod{expectedHeader: "bar"},
	)
	require.True(t, ok)
	require.NoError(t, err)
	require.Equal(t, syntax.DID("did:web:test"), did)

	// Matching method fails.
	_, ok, err = ValidateRaw(
		ctx,
		"bar",
		&testAuthMethod{expectedHeader: "foo"},
		&testAuthMethod{expectedHeader: "bar", fail: true},
	)
	require.False(t, ok)
	require.Error(t, err)

	// No method matches the token.
	_, ok, err = ValidateRaw(
		ctx,
		"foo",
		&testAuthMethod{expectedHeader: "bar"},
	)
	require.False(t, ok)
	require.Error(t, err)
}
