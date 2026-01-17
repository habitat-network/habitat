package oauthserver

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewJWTSession_SubjectIsSet(t *testing.T) {
	// Create an authSession with a test DID
	testDID := "did:web:test.example.com"
	authSess := &authSession{
		Subject:   testDID,
		ExpiresAt: time.Now().Add(time.Hour),
		Scopes:    []string{"read", "write"},
	}

	// Create JWT session
	jwtSession := newJWTSession(authSess)

	// Verify the Subject field is set (this is what GetSubject() returns)
	require.Equal(t, testDID, jwtSession.Subject, "JWTSession.Subject should be set")
	require.Equal(t, testDID, jwtSession.GetSubject(), "GetSubject() should return the DID")

	// Verify JWTClaims.Subject is also set (this goes into the JWT token)
	require.NotNil(t, jwtSession.JWTClaims, "JWTClaims should not be nil")
	require.Equal(t, testDID, jwtSession.JWTClaims.Subject, "JWTClaims.Subject should be set")
}
