package oauthserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHasJSONBody(t *testing.T) {
	for _, tt := range []struct {
		contentType string
		want        bool
	}{
		{"application/json", true},
		{"application/json; charset=utf-8", true},
		{"application/x-www-form-urlencoded", false},
		{"", false},
		{"not/a/media/type", false},
	} {
		r := httptest.NewRequest(http.MethodPost, "/oauth/par", strings.NewReader("{}"))
		if tt.contentType != "" {
			r.Header.Set("Content-Type", tt.contentType)
		}
		require.Equal(t, tt.want, hasJSONBody(r), tt.contentType)
	}
}

// TestPARRequestBodyFormValues uses the exact body pds.ls sends.
func TestPARRequestBodyFormValues(t *testing.T) {
	raw := `{"redirect_uri":"https://pds.ls/","code_challenge":"qYS44tmxT53yZcvEDo8njG4G6KIqXmNGOxcrkz5Q1xs","code_challenge_method":"S256","state":"P9VSOQdpuVojZs15GEhV2Ne-","login_hint":"admin.acmecorp.pear.local.habitat.network","response_mode":"fragment","response_type":"code","scope":"atproto repo:*?action=create","client_id":"https://pds.ls/oauth-client-metadata.json"}`

	var body parRequestBody
	require.NoError(t, json.Unmarshal([]byte(raw), &body))

	require.Equal(t, url.Values{
		"redirect_uri":          {"https://pds.ls/"},
		"code_challenge":        {"qYS44tmxT53yZcvEDo8njG4G6KIqXmNGOxcrkz5Q1xs"},
		"code_challenge_method": {"S256"},
		"state":                 {"P9VSOQdpuVojZs15GEhV2Ne-"},
		"login_hint":            {"admin.acmecorp.pear.local.habitat.network"},
		"response_mode":         {"fragment"},
		"response_type":         {"code"},
		"scope":                 {"atproto repo:*?action=create"},
		"client_id":             {"https://pds.ls/oauth-client-metadata.json"},
	}, body.formValues())
}

func TestTokenRequestBodyFormValues(t *testing.T) {
	raw := `{"client_id":"https://pds.ls/oauth-client-metadata.json","grant_type":"authorization_code","redirect_uri":"https://pds.ls/","code":"ory_ac_abc","code_verifier":"verifier"}`

	var body tokenRequestBody
	require.NoError(t, json.Unmarshal([]byte(raw), &body))

	require.Equal(t, url.Values{
		"client_id":     {"https://pds.ls/oauth-client-metadata.json"},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {"https://pds.ls/"},
		"code":          {"ory_ac_abc"},
		"code_verifier": {"verifier"},
	}, body.formValues())
}

func TestTokenRequestBodyFormValuesRefresh(t *testing.T) {
	raw := `{"client_id":"abc","grant_type":"refresh_token","refresh_token":"rt"}`

	var body tokenRequestBody
	require.NoError(t, json.Unmarshal([]byte(raw), &body))

	require.Equal(t, url.Values{
		"client_id":     {"abc"},
		"grant_type":    {"refresh_token"},
		"refresh_token": {"rt"},
	}, body.formValues())
}

// TestPARFormSurvivesParseMultipartForm covers the contract HandlePAR
// depends on: fosite's PAR handler calls ParseMultipartForm (which leaves a
// non-nil r.Form alone and then returns ErrNotMultipart, ignored by fosite)
// and reads r.Form.
func TestPARFormSurvivesParseMultipartForm(t *testing.T) {
	r := httptest.NewRequest(
		http.MethodPost,
		"/oauth/par",
		strings.NewReader(`{"client_id":"abc"}`),
	)
	r.Header.Set("Content-Type", "application/json")

	r.Form = url.Values{"client_id": {"abc"}}

	require.ErrorIs(t, r.ParseMultipartForm(1<<20), http.ErrNotMultipart)
	require.Equal(t, "abc", r.Form.Get("client_id"))
}

// TestTokenPostFormSurvivesParseMultipartForm covers the contract
// HandleToken depends on: fosite's token handler reads r.PostForm, and
// ParseForm derives r.Form from it plus the URL query instead of parsing the
// JSON body.
func TestTokenPostFormSurvivesParseMultipartForm(t *testing.T) {
	r := httptest.NewRequest(
		http.MethodPost,
		"/oauth/token?from_query=yes",
		strings.NewReader(`{"grant_type":"authorization_code"}`),
	)
	r.Header.Set("Content-Type", "application/json")

	r.PostForm = url.Values{"grant_type": {"authorization_code"}}

	require.ErrorIs(t, r.ParseMultipartForm(1<<20), http.ErrNotMultipart)
	require.Equal(t, "authorization_code", r.PostForm.Get("grant_type"))
	// Query params are not body params, but do land in r.Form.
	require.Empty(t, r.PostForm.Get("from_query"))
	require.Equal(t, "authorization_code", r.Form.Get("grant_type"))
	require.Equal(t, "yes", r.Form.Get("from_query"))
}
