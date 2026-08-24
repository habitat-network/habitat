package oauthserver

import (
	"mime"
	"net/http"
	"net/url"
)

type parRequestBody struct {
	ClientID     string `json:"client_id"`
	State        string `json:"state"`
	RedirectURI  string `json:"redirect_uri"`
	Scope        string `json:"scope"`
	LoginHint    string `json:"login_hint"`
	Prompt       string `json:"prompt"`
	ResponseType string `json:"response_type"`
	// ResponseMode is absent from indigo's PushedAuthRequest but browser
	// atproto clients send "fragment" and read the code out of location.hash.
	ResponseMode        string `json:"response_mode"`
	ClientAssertionType string `json:"client_assertion_type"`
	ClientAssertion     string `json:"client_assertion"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
}

func (b parRequestBody) formValues() url.Values {
	return nonEmptyValues(map[string]string{
		"client_id":             b.ClientID,
		"state":                 b.State,
		"redirect_uri":          b.RedirectURI,
		"scope":                 b.Scope,
		"login_hint":            b.LoginHint,
		"prompt":                b.Prompt,
		"response_type":         b.ResponseType,
		"response_mode":         b.ResponseMode,
		"client_assertion_type": b.ClientAssertionType,
		"client_assertion":      b.ClientAssertion,
		"code_challenge":        b.CodeChallenge,
		"code_challenge_method": b.CodeChallengeMethod,
	})
}

// tokenRequestBody is the JSON form of a token request. It covers both the
// initial authorization_code exchange and refresh_token grants.
type tokenRequestBody struct {
	ClientID            string `json:"client_id"`
	GrantType           string `json:"grant_type"`
	RedirectURI         string `json:"redirect_uri"`
	Code                string `json:"code"`
	CodeVerifier        string `json:"code_verifier"`
	RefreshToken        string `json:"refresh_token"`
	Scope               string `json:"scope"`
	ClientAssertionType string `json:"client_assertion_type"`
	ClientAssertion     string `json:"client_assertion"`
}

func (b tokenRequestBody) formValues() url.Values {
	return nonEmptyValues(map[string]string{
		"client_id":             b.ClientID,
		"grant_type":            b.GrantType,
		"redirect_uri":          b.RedirectURI,
		"code":                  b.Code,
		"code_verifier":         b.CodeVerifier,
		"refresh_token":         b.RefreshToken,
		"scope":                 b.Scope,
		"client_assertion_type": b.ClientAssertionType,
		"client_assertion":      b.ClientAssertion,
	})
}

func nonEmptyValues(fields map[string]string) url.Values {
	values := url.Values{}
	for key, value := range fields {
		if value != "" {
			values.Set(key, value)
		}
	}
	return values
}

// hasJSONBody reports whether the request carries a JSON body rather than the
// form-encoded one fosite expects.
func hasJSONBody(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mediaType == "application/json"
}
