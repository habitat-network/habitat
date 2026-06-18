package instanceadmin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func newTestServer(t *testing.T) (*Server, Store, string) {
	t.Helper()
	store := newTestStore(t)
	password, generated, err := store.Bootstrap(t.Context(), "")
	require.NoError(t, err)
	require.True(t, generated)
	return NewServer(store), store, password
}

func TestServeLoginPage(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	rec := httptest.NewRecorder()
	server.ServeLoginPage(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "<form")
}

func TestHandleLogin_Success(t *testing.T) {
	server, _, password := newTestServer(t)

	form := url.Values{"password": {password}}
	req := httptest.NewRequest(
		http.MethodPost,
		"/admin/login",
		strings.NewReader(form.Encode()),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	server.HandleLogin(rec, req)

	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Equal(t, "/admin", rec.Header().Get("Location"))

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, sessionName, cookies[0].Name)
	require.NotEmpty(t, cookies[0].Value)
	require.True(t, cookies[0].HttpOnly)
}

func TestHandleLogin_WrongPassword(t *testing.T) {
	server, _, _ := newTestServer(t)

	form := url.Values{"password": {"wrong-password"}}
	req := httptest.NewRequest(
		http.MethodPost,
		"/admin/login",
		strings.NewReader(form.Encode()),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	server.HandleLogin(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Empty(t, rec.Result().Cookies())
}

func TestRequireSession_RedirectsWithoutCookie(t *testing.T) {
	server, _, _ := newTestServer(t)

	called := false
	handler := server.RequireSession(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	require.False(t, called)
	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Equal(t, "/admin/login", rec.Header().Get("Location"))
}

func TestRequireSession_PassesThroughWithValidCookie(t *testing.T) {
	server, store, _ := newTestServer(t)

	createRec := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/admin/login", nil)
	require.NoError(t, store.CreateSession(createRec, createReq))
	cookies := createRec.Result().Cookies()
	require.Len(t, cookies, 1)

	called := false
	handler := server.RequireSession(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.AddCookie(cookies[0])
	rec := httptest.NewRecorder()
	handler(rec, req)

	require.True(t, called)
}

func TestHandleLogout_ClearsSessionAndCookie(t *testing.T) {
	server, store, _ := newTestServer(t)

	createRec := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/admin/login", nil)
	require.NoError(t, store.CreateSession(createRec, createReq))
	sessionCookies := createRec.Result().Cookies()
	require.Len(t, sessionCookies, 1)

	req := httptest.NewRequest(http.MethodPost, "/admin/logout", nil)
	req.AddCookie(sessionCookies[0])
	rec := httptest.NewRecorder()
	server.HandleLogout(rec, req)

	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Equal(t, "/admin/login", rec.Header().Get("Location"))

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	require.LessOrEqual(t, cookies[0].MaxAge, 0)

	finalReq := httptest.NewRequest(http.MethodGet, "/admin", nil)
	finalReq.AddCookie(cookies[0])
	ok, err := store.ValidateSession(finalReq)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestServeAdminHome(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	server.ServeAdminHome(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "instance admin")
}
