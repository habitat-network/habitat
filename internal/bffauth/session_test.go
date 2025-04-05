package bffauth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInMemorySessionPersister(t *testing.T) {
	persister := NewInMemorySessionPersister()

	// Test saving and retrieving a session
	session := &ChallengeSession{
		SessionID: "test-session",
		DID:       "did:test:123",
		Challenge: "test-challenge",
		ExpiresAt: time.Now().Add(15 * time.Minute),
	}

	err := persister.SaveSession(session)
	require.NoError(t, err)

	// Test retrieving the session
	retrieved, err := persister.GetSession(session.SessionID)
	require.NoError(t, err)
	require.Equal(t, session.SessionID, retrieved.SessionID)
	require.Equal(t, session.DID, retrieved.DID)
	require.Equal(t, session.Challenge, retrieved.Challenge)
	require.Equal(t, session.ExpiresAt.Unix(), retrieved.ExpiresAt.Unix())

	// Test retrieving non-existent session
	_, err = persister.GetSession("non-existent")
	require.Error(t, err)

	// Test deleting a session
	err = persister.DeleteSession(session.SessionID)
	require.NoError(t, err)

	// Verify session was deleted
	_, err = persister.GetSession(session.SessionID)
	require.Error(t, err)

	// Test deleting non-existent session
	err = persister.DeleteSession("non-existent")
	require.NoError(t, err)
}
