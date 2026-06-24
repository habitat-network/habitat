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
	supportedCreds := []CredentialType{OrgCredential}
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	r.Header.Set("Authorization", "foo")
	credInfo, ok := NewValidator(
		WithAuthMethods(&testAuthMethod{expectedHeader: "foo"}, &testAuthMethod{expectedHeader: "bar"}),
		WithSupportedCredentials(supportedCreds...),
	).Validate(w, r)
	require.True(t, ok)
	require.Equal(t, syntax.DID("did:web:test"), credInfo.Subject)

	w = httptest.NewRecorder()
	r.Header.Set("Authorization", "bar")
	_, ok = NewValidator(
		WithAuthMethods(&testAuthMethod{expectedHeader: "foo"}, &testAuthMethod{expectedHeader: "bar", fail: true}),
		WithSupportedCredentials(supportedCreds...),
	).Validate(w, r)
	require.False(t, ok)
	require.Equal(t, w.Result().StatusCode, http.StatusUnauthorized)

	w = httptest.NewRecorder()
	r.Header.Set("Authorization", "foo")
	_, ok = NewValidator(
		WithAuthMethods(&testAuthMethod{expectedHeader: "bar"}),
		WithSupportedCredentials(supportedCreds...),
	).Validate(w, r)
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
) (*CredentialInfo, bool) {
	if t.fail {
		w.WriteHeader(http.StatusUnauthorized)
		return nil, false
	}
	return &CredentialInfo{Subject: syntax.DID("did:web:test"), Type: OrgCredential}, true
}

// ValidateRaw implements [Method].
func (t *testAuthMethod) ValidateRaw(
	ctx context.Context,
	token string,
	scopes ...string,
) (*CredentialInfo, bool, error) {
	if token != t.expectedHeader {
		return nil, false, fmt.Errorf("unexpected token")
	}
	if t.fail {
		return nil, false, fmt.Errorf("auth failed")
	}
	return &CredentialInfo{Subject: syntax.DID("did:web:test"), Type: OrgCredential}, true, nil
}

func TestValidateRaw(t *testing.T) {
	ctx := context.Background()

	// First method handles it successfully.
	credInfo, ok, err := NewValidator(
		WithAuthMethods(&testAuthMethod{expectedHeader: "foo"}, &testAuthMethod{expectedHeader: "bar"}),
	).ValidateRaw(ctx, "foo")
	require.True(t, ok)
	require.NoError(t, err)
	require.Equal(t, syntax.DID("did:web:test"), credInfo.Subject)

	// Matching method fails.
	_, ok, err = NewValidator(
		WithAuthMethods(&testAuthMethod{expectedHeader: "foo"}, &testAuthMethod{expectedHeader: "bar", fail: true}),
	).ValidateRaw(ctx, "bar")
	require.False(t, ok)
	require.Error(t, err)

	// No method matches the token.
	_, ok, err = NewValidator(
		WithAuthMethods(&testAuthMethod{expectedHeader: "bar"}),
	).ValidateRaw(ctx, "foo")
	require.False(t, ok)
	require.Error(t, err)
}
