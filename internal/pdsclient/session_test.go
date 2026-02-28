package pdsclient

import (
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/gorilla/sessions"
	"github.com/stretchr/testify/require"
)

func TestSessionInterface(t *testing.T) {
	var _ Session = &cookieSession{}
}

func TestSession(t *testing.T) {
	// Setup
	sessionStore := sessions.NewCookieStore([]byte("test-key"))
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	// Create test data
	testIdentity := &identity.Identity{
		DID:    "did:plc:test123",
		Handle: "test.bsky.app",
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {
				Type: "AtprotoPersonalDataServer",
				URL:  "https://test.pds.com",
			},
		},
	}

	testTokenInfo := &TokenResponse{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		Scope:        "atproto transition:generic",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
	}

	// Step 1: Create a new session
	session, err := newCookieSession(req, sessionStore, testIdentity, "https://test.pds.com")
	require.NoError(t, err)
	require.NotNil(t, session)

	// Get the key that was generated during session creation
	originalKey, ok, err := session.GetDpopKey()
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, originalKey)

	// Step 2: Set additional fields
	err = session.SetTokenInfo(testTokenInfo)
	require.NoError(t, err)

	err = session.SetIssuer("https://test.issuer.com")
	require.NoError(t, err)

	err = session.SetDpopNonce("test-nonce-123")
	require.NoError(t, err)

	// Step 3: Save the session to get cookies
	session.Save(req, w)

	// Step 4: Extract cookies from the response
	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, SessionKeyDpop, cookies[0].Name)

	// Step 5: Create a new request with the session cookie
	newReq := httptest.NewRequest("GET", "/test", nil)
	newReq.AddCookie(cookies[0])

	// Step 6: Retrieve the existing session
	retrievedSession, err := getCookieSession(newReq, sessionStore)
	require.NoError(t, err)
	require.NotNil(t, retrievedSession)

	// Step 7: Verify all fields persisted correctly

	// Verify DPoP key
	retrievedKey, ok, err := retrievedSession.GetDpopKey()
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, retrievedKey)
	require.Equal(t, originalKey.D, retrievedKey.D)
	require.Equal(t, originalKey.X, retrievedKey.X)
	require.Equal(t, originalKey.Y, retrievedKey.Y)

	// Verify identity
	retrievedIdentity, ok, err := retrievedSession.GetIdentity()
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, retrievedIdentity)
	require.Equal(t, testIdentity.DID, retrievedIdentity.DID)
	require.Equal(t, testIdentity.Handle, retrievedIdentity.Handle)
	require.Equal(
		t,
		testIdentity.Services["atproto_pds"].URL,
		retrievedIdentity.Services["atproto_pds"].URL,
	)

	// Verify PDS URL
	retrievedPDSURL, ok, err := retrievedSession.GetPDSURL()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "https://test.pds.com", retrievedPDSURL)

	// Verify token info
	retrievedTokenInfo, ok, err := retrievedSession.GetTokenInfo()
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, retrievedTokenInfo)
	require.Equal(t, testTokenInfo.AccessToken, retrievedTokenInfo.AccessToken)
	require.Equal(t, testTokenInfo.RefreshToken, retrievedTokenInfo.RefreshToken)
	require.Equal(t, testTokenInfo.Scope, retrievedTokenInfo.Scope)
	require.Equal(t, testTokenInfo.TokenType, retrievedTokenInfo.TokenType)
	require.Equal(t, testTokenInfo.ExpiresIn, retrievedTokenInfo.ExpiresIn)

	// Verify issuer
	retrievedIssuer, ok, err := retrievedSession.GetIssuer()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "https://test.issuer.com", retrievedIssuer)

	// Verify nonce
	retrievedNonce, ok, err := retrievedSession.GetDpopNonce()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "test-nonce-123", retrievedNonce)
}
