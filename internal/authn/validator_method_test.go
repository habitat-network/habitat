package authn

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

// fixedMethod is a Method that always handles the request and always returns
// the same *CredentialInfo pointer, the way testutil.NewSuccessValidator does.
// EndpointOptions.Validate must not write through that pointer.
type fixedMethod struct {
	info *CredentialInfo
}

func (f *fixedMethod) CanHandle(*http.Request) bool { return true }

func (f *fixedMethod) Validate(
	http.ResponseWriter, *http.Request, ...string,
) (*CredentialInfo, bool) {
	return f.info, true
}

// cantHandleMethod stands in for an auth method the request did not use.
// testutil.NewCantHandleMethod is unavailable here: that package imports this
// one, so an in-package test cannot import it back.
type cantHandleMethod struct{}

func (c *cantHandleMethod) CanHandle(*http.Request) bool { return false }

func (c *cantHandleMethod) Validate(
	http.ResponseWriter, *http.Request, ...string,
) (*CredentialInfo, bool) {
	return nil, false
}

func TestValidator_StampsCredentialMethod(t *testing.T) {
	subject := syntax.DID("did:plc:alice")

	validate := func(t *testing.T, v *validator) (*CredentialInfo, bool) {
		t.Helper()
		return v.Request(
			WithMethods(ValidatorMethodOAuth, ValidatorMethodServiceAuth),
		).Validate(httptest.NewRecorder(), newTestRequest(t, "unused"))
	}

	t.Run("stamps the method that validated", func(t *testing.T) {
		tests := []struct {
			name    string
			oauth   Method
			service Method
			want    ValidatorMethod
		}{
			{
				name:    "oauth",
				oauth:   &fixedMethod{info: &CredentialInfo{Subject: subject}},
				service: &cantHandleMethod{},
				want:    ValidatorMethodOAuth,
			},
			{
				name:    "service auth",
				oauth:   &cantHandleMethod{},
				service: &fixedMethod{info: &CredentialInfo{Subject: subject}},
				want:    ValidatorMethodServiceAuth,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				cred, ok := validate(
					t, &validator{oauth: tt.oauth, serviceAuth: tt.service},
				)

				require.True(t, ok)
				require.Equal(t, tt.want, cred.Method)
				require.Equal(t, subject, cred.Subject)
			})
		}
	})

	t.Run("an unstamped credential has no method", func(t *testing.T) {
		require.Equal(t, ValidatorMethodNone, CredentialInfo{}.Method)
	})

	t.Run("does not write through the method's credential", func(t *testing.T) {
		shared := &CredentialInfo{Subject: subject}
		v := &validator{
			oauth:       &cantHandleMethod{},
			serviceAuth: &fixedMethod{info: shared},
		}

		cred, ok := validate(t, v)

		require.True(t, ok)
		require.Equal(t, ValidatorMethodServiceAuth, cred.Method)
		require.Equal(
			t, ValidatorMethodNone, shared.Method,
			"the method's own credential must be left untouched",
		)
	})
}
