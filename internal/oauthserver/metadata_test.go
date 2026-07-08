package oauthserver

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

const testIssuer = "https://habitat.example"

func TestBuildAuthServerMetadata(t *testing.T) {
	b, err := json.Marshal(buildAuthServerMetadata(testIssuer))
	require.NoError(t, err)
	require.JSONEq(t, `{
		"issuer": "https://habitat.example",
		"authorization_endpoint": "https://habitat.example/oauth/authorize",
		"token_endpoint": "https://habitat.example/oauth/token",
		"pushed_authorization_request_endpoint": "https://habitat.example/oauth/par",
		"response_types_supported": ["code"],
		"grant_types_supported": [
		"authorization_code",
		"refresh_token",
		"urn:ietf:params:oauth:grant-type:jwt-bearer"
		],
		"code_challenge_methods_supported": ["S256"],
		"token_endpoint_auth_methods_supported": ["none", "private_key_jwt"],
		"token_endpoint_auth_signing_alg_values_supported": ["ES256"],
		"scopes_supported": ["atproto"],
		"dpop_signing_alg_values_supported": ["ES256"],
		"authorization_response_iss_parameter_supported": true,
		"require_pushed_authorization_requests": true,
		"client_id_metadata_document_supported": true
		}`, string(b))
}

func TestBuildProtectedResourceMetadata(t *testing.T) {
	b, err := json.Marshal(buildProtectedResourceMetadata(testIssuer))
	require.NoError(t, err)
	require.JSONEq(t, `{ "authorization_servers": ["https://habitat.example"] }`, string(b))
}
