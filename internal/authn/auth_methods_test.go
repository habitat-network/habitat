package authn

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

func TestValidate(t *testing.T) {
	r := httptest.NewRequest("GET", "/", http.NoBody)
	w := httptest.NewRecorder()
	r.Header.Set("Authorization", "foo")
	credInfo, ok := NewValidator(
		&testAuthMethod{expectedHeader: "foo"},
		nil,
		nil,
		nil,
		nil,
	).Request(WithMethods(ValidatorMethodOAuth)).Validate(w, r)
	require.True(t, ok)
	require.Equal(t, syntax.DID("did:web:test"), credInfo.Subject)

	w = httptest.NewRecorder()
	r.Header.Set("Authorization", "bar")
	_, ok = NewValidator(
		&testAuthMethod{expectedHeader: "bar", fail: true},
		nil,
		nil,
		nil,
		nil,
	).Request(WithMethods(ValidatorMethodOAuth)).Validate(w, r)
	require.False(t, ok)
	require.Equal(t, w.Result().StatusCode, http.StatusUnauthorized)

	w = httptest.NewRecorder()
	r.Header.Set("Authorization", "foo")
	_, ok = NewValidator(
		&testAuthMethod{expectedHeader: "bar"},
		nil,
		nil,
		nil,
		nil,
	).Request(WithMethods(ValidatorMethodOAuth)).Validate(w, r)
	require.False(t, ok)
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
