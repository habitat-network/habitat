package instanceadmin

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	store, err := NewStore(db)
	require.NoError(t, err)
	return store
}

func TestBootstrap_GeneratesPasswordOnFirstCall(t *testing.T) {
	s := newTestStore(t)

	password, created, err := s.Bootstrap(t.Context(), "")
	require.NoError(t, err)
	require.True(t, created)
	require.NotEmpty(t, password)

	ok, err := s.Authenticate(t.Context(), password)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestBootstrap_SecondCallDoesNotRegenerate(t *testing.T) {
	s := newTestStore(t)

	firstPassword, created, err := s.Bootstrap(t.Context(), "")
	require.NoError(t, err)
	require.True(t, created)

	secondPassword, created, err := s.Bootstrap(t.Context(), "")
	require.NoError(t, err)
	require.False(t, created)
	require.Empty(t, secondPassword)

	// Original password still works.
	ok, err := s.Authenticate(t.Context(), firstPassword)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestBootstrap_UsesPresetPasswordWithoutReturningIt(t *testing.T) {
	s := newTestStore(t)

	password, created, err := s.Bootstrap(t.Context(), "preset-password-123")
	require.NoError(t, err)
	require.True(t, created)
	require.Empty(t, password)

	ok, err := s.Authenticate(t.Context(), "preset-password-123")
	require.NoError(t, err)
	require.True(t, ok)
}

func TestBootstrap_PresetIgnoredOnSubsequentCalls(t *testing.T) {
	s := newTestStore(t)

	_, _, err := s.Bootstrap(t.Context(), "first-password")
	require.NoError(t, err)

	_, created, err := s.Bootstrap(t.Context(), "second-password")
	require.NoError(t, err)
	require.False(t, created)

	ok, err := s.Authenticate(t.Context(), "first-password")
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = s.Authenticate(t.Context(), "second-password")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestAuthenticate_WrongPasswordFails(t *testing.T) {
	s := newTestStore(t)

	_, _, err := s.Bootstrap(t.Context(), "correct-password")
	require.NoError(t, err)

	ok, err := s.Authenticate(t.Context(), "wrong-password")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestSession_CreateValidateDelete(t *testing.T) {
	s := newTestStore(t)

	token, expiresAt, err := s.CreateSession(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, token)
	require.True(t, expiresAt.After(time.Now()))

	ok, err := s.ValidateSession(t.Context(), token)
	require.NoError(t, err)
	require.True(t, ok)

	err = s.DeleteSession(t.Context(), token)
	require.NoError(t, err)

	ok, err = s.ValidateSession(t.Context(), token)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestValidateSession_UnknownTokenFails(t *testing.T) {
	s := newTestStore(t)

	ok, err := s.ValidateSession(t.Context(), "does-not-exist")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestValidateSession_ExpiredSessionFails(t *testing.T) {
	s := newTestStore(t).(*storeImpl)

	token, _, err := s.CreateSession(t.Context())
	require.NoError(t, err)

	// Force the session into the past.
	err = s.db.WithContext(t.Context()).
		Model(&instanceAdminSession{}).
		Where("token = ?", token).
		Update("expires_at", time.Now().Add(-time.Hour)).Error
	require.NoError(t, err)

	ok, err := s.ValidateSession(t.Context(), token)
	require.NoError(t, err)
	require.False(t, ok)
}
