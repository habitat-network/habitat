package instanceadmin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexedwards/argon2id"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestStore(t *testing.T) Store {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)

	hash, err := argon2id.CreateHash("password", argon2id.DefaultParams)
	require.NoError(t, err)

	store, err := NewStore(db, []byte("random"), "pear.example.com", hash)
	require.NoError(t, err)
	return store
}

func TestAuthenticate_WrongPasswordFails(t *testing.T) {
	s := newTestStore(t)

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

func TestGetSettings_DefaultsOnFirstAccess(t *testing.T) {
	s := newTestStore(t)

	instanceName, policy, err := s.GetSettings(t.Context())
	require.NoError(t, err)
	require.Equal(t, "", instanceName)
	require.Equal(t, "open", policy)
}

func TestUpdateSettings_PersistsAndReads(t *testing.T) {
	s := newTestStore(t)

	err := s.UpdateSettings(t.Context(), "Acme Hosting", "invite_only")
	require.NoError(t, err)

	instanceName, policy, err := s.GetSettings(t.Context())
	require.NoError(t, err)
	require.Equal(t, "Acme Hosting", instanceName)
	require.Equal(t, "invite_only", policy)
}

func TestUpdateSettings_RejectsInvalidPolicy(t *testing.T) {
	s := newTestStore(t)

	err := s.UpdateSettings(t.Context(), "Acme Hosting", "sometimes")
	require.ErrorIs(t, err, ErrInvalidPolicy)
}

func TestGetOrgCreationPolicy_DefaultsToOpen(t *testing.T) {
	s := newTestStore(t)

	policy, err := s.GetOrgCreationPolicy(t.Context())
	require.NoError(t, err)
	require.Equal(t, "open", policy)
}

func TestGetOrgCreationPolicy_ReflectsUpdate(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.UpdateSettings(t.Context(), "Acme Hosting", "invite_only"))

	policy, err := s.GetOrgCreationPolicy(t.Context())
	require.NoError(t, err)
	require.Equal(t, "invite_only", policy)
}

func TestIssueInvite_CanBeRedeemedOnce(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.UpdateSettings(t.Context(), "Acme Hosting", "invite_only"))

	token, err := s.IssueInvite(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, token)

	require.NoError(t, s.ValidateInvite(t.Context(), token))
	require.NoError(t, s.MarkInviteUsed(t.Context(), token))

	err = s.ValidateInvite(t.Context(), token)
	require.ErrorIs(t, err, ErrInvalidInvite)
}

func TestValidateInvite_DoesNotConsumeToken(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.UpdateSettings(t.Context(), "Acme Hosting", "invite_only"))

	token, err := s.IssueInvite(t.Context())
	require.NoError(t, err)

	// Validating multiple times should not consume the invite.
	require.NoError(t, s.ValidateInvite(t.Context(), token))
	require.NoError(t, s.ValidateInvite(t.Context(), token))

	// It can still be marked used afterwards.
	require.NoError(t, s.MarkInviteUsed(t.Context(), token))
}

func TestRedeemInvite_UnknownTokenFails(t *testing.T) {
	s := newTestStore(t)

	err := s.ValidateInvite(t.Context(), "not-a-real-token")
	require.ErrorIs(t, err, ErrInvalidInvite)

	err = s.MarkInviteUsed(t.Context(), "not-a-real-token")
	require.ErrorIs(t, err, ErrInvalidInvite)
}

func TestRedeemInvite_WrongSigningSecretFails(t *testing.T) {
	s1 := newTestStore(t)
	require.NoError(t, s1.UpdateSettings(t.Context(), "Acme Hosting", "invite_only"))
	token, err := s1.IssueInvite(t.Context())
	require.NoError(t, err)

	// A different store (different generated signing secret) must reject it.
	s2 := newTestStore(t)
	err = s2.ValidateInvite(t.Context(), token)
	require.ErrorIs(t, err, ErrInvalidInvite)
}
