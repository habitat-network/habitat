package instanceadmin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	store, err := NewStore([]byte("random"))
	require.NoError(t, err)
	return store
}

func TestBootstrap_GeneratesPasswordWhenNoPresetGiven(t *testing.T) {
	s := newTestStore(t)

	password, generated, err := s.Bootstrap(t.Context(), "")
	require.NoError(t, err)
	require.True(t, generated)
	require.NotEmpty(t, password)

	ok, err := s.Authenticate(t.Context(), password)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestBootstrap_UsesPresetPasswordWithoutReturningIt(t *testing.T) {
	s := newTestStore(t)

	password, generated, err := s.Bootstrap(t.Context(), "preset-password-123")
	require.NoError(t, err)
	require.False(t, generated)
	require.Empty(t, password)

	ok, err := s.Authenticate(t.Context(), "preset-password-123")
	require.NoError(t, err)
	require.True(t, ok)
}

func TestBootstrap_SubsequentCallReplacesCredential(t *testing.T) {
	s := newTestStore(t)

	firstPassword, _, err := s.Bootstrap(t.Context(), "")
	require.NoError(t, err)

	_, _, err = s.Bootstrap(t.Context(), "second-password")
	require.NoError(t, err)

	// The first credential no longer works once Bootstrap is called again.
	ok, err := s.Authenticate(t.Context(), firstPassword)
	require.NoError(t, err)
	require.False(t, ok)

	ok, err = s.Authenticate(t.Context(), "second-password")
	require.NoError(t, err)
	require.True(t, ok)
}

func TestAuthenticate_BeforeBootstrapFails(t *testing.T) {
	s := newTestStore(t)

	ok, err := s.Authenticate(t.Context(), "anything")
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

	createRec := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/admin/login", nil)
	require.NoError(t, s.CreateSession(createRec, createReq))
	cookies := createRec.Result().Cookies()
	require.Len(t, cookies, 1)

	validateReq := httptest.NewRequest(http.MethodGet, "/admin", nil)
	validateReq.AddCookie(cookies[0])
	ok, err := s.ValidateSession(validateReq)
	require.NoError(t, err)
	require.True(t, ok)

	deleteRec := httptest.NewRecorder()
	require.NoError(t, s.DeleteSession(deleteRec, validateReq))
	deleteCookies := deleteRec.Result().Cookies()
	require.Len(t, deleteCookies, 1)
	require.LessOrEqual(t, deleteCookies[0].MaxAge, 0)

	finalReq := httptest.NewRequest(http.MethodGet, "/admin", nil)
	finalReq.AddCookie(deleteCookies[0])
	ok, err = s.ValidateSession(finalReq)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestValidateSession_NoCookieFails(t *testing.T) {
	s := newTestStore(t)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	ok, err := s.ValidateSession(req)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestValidateSession_TamperedCookieFails(t *testing.T) {
	s := newTestStore(t)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.AddCookie(&http.Cookie{Name: sessionName, Value: "not-a-valid-session-value"})
	ok, err := s.ValidateSession(req)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestNewStore_SessionCookieOptions(t *testing.T) {
	s := newTestStore(t).(*storeImpl)

	require.Equal(t, "/admin", s.sessions.Options.Path)
	require.Equal(t, int(sessionDuration.Seconds()), s.sessions.Options.MaxAge)
	require.True(t, s.sessions.Options.HttpOnly)
}
