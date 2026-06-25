package instance

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/stretchr/testify/require"
)

func newTestServer(t *testing.T) (*Server, AdminStore, string) {
	t.Helper()
	store := newTestStore(t)
	return NewServer(store, "https://frontend.example"), store, "password"
}

// sessionCookie creates a new session in store and returns its cookie.
func sessionCookie(t *testing.T, store AdminStore) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/login", nil)
	require.NoError(t, store.CreateSession(rec, req))
	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	return cookies[0]
}

func TestServeLoginPage(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	rec := httptest.NewRecorder()
	server.ServeLoginPage(rec, req)

	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Equal(t, "/ui/admin/login", rec.Header().Get("Location"))
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
	require.Equal(t, "/ui/admin", rec.Header().Get("Location"))

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

	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Contains(t, rec.Header().Get("Location"), "/ui/admin/login?error=")
	require.Empty(t, rec.Result().Cookies())
}

func TestRequireSessionAPI_FailsWithoutCookie(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.admin.getSettings", nil)
	rec := httptest.NewRecorder()
	ok := server.requireSessionAPI(rec, req)

	require.False(t, ok)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireSessionAPI_PassesWithValidCookie(t *testing.T) {
	server, store, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.AddCookie(sessionCookie(t, store))
	rec := httptest.NewRecorder()
	ok := server.requireSessionAPI(rec, req)

	require.True(t, ok)
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
	require.Equal(t, "/ui/admin/login", rec.Header().Get("Location"))

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	require.LessOrEqual(t, cookies[0].MaxAge, 0)

	finalReq := httptest.NewRequest(http.MethodGet, "/admin", nil)
	finalReq.AddCookie(cookies[0])
	ok, err := store.ValidateSession(finalReq)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestServeAdminHome_RedirectsToEmbeddedPage(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	server.ServeAdminHome(rec, req)

	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Equal(t, "/ui/admin", rec.Header().Get("Location"))
}

func TestServeConfig_RequiresSession(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/config", nil)
	rec := httptest.NewRecorder()
	server.GetSettings(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetSettings_RequiresSession(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.admin.getSettings", nil)
	rec := httptest.NewRecorder()
	server.GetSettings(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetSettings_ReturnsDefaults(t *testing.T) {
	server, store, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.admin.getSettings", nil)
	req.AddCookie(sessionCookie(t, store))
	rec := httptest.NewRecorder()
	server.GetSettings(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var out habitat.NetworkHabitatAdminGetSettingsOutput
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	require.Equal(t, "open", out.OrgCreationPolicy)
}

func TestUpdateSettings_PersistsAndReturnsUpdated(t *testing.T) {
	server, store, _ := newTestServer(t)

	body, _ := json.Marshal(habitat.NetworkHabitatAdminUpdateSettingsInput{
		InstanceName:      "Acme Hosting",
		OrgCreationPolicy: "invite_only",
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.admin.updateSettings",
		bytes.NewReader(body),
	)
	req.AddCookie(sessionCookie(t, store))
	rec := httptest.NewRecorder()
	server.UpdateSettings(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var out habitat.NetworkHabitatAdminUpdateSettingsOutput
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	require.Equal(t, "Acme Hosting", out.InstanceName)
	require.Equal(t, "invite_only", out.OrgCreationPolicy)
}

func TestUpdateSettings_InvalidPolicyReturns400(t *testing.T) {
	server, store, _ := newTestServer(t)

	body, _ := json.Marshal(habitat.NetworkHabitatAdminUpdateSettingsInput{
		InstanceName:      "Acme Hosting",
		OrgCreationPolicy: "sometimes",
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.admin.updateSettings",
		bytes.NewReader(body),
	)
	req.AddCookie(sessionCookie(t, store))
	rec := httptest.NewRecorder()
	server.UpdateSettings(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUpdateSettings_OmittedFieldLeavesItUnchanged(t *testing.T) {
	server, store, _ := newTestServer(t)
	require.NoError(t, store.UpdateSettings(t.Context(), "Acme Hosting", "invite_only"))

	body, _ := json.Marshal(habitat.NetworkHabitatAdminUpdateSettingsInput{
		InstanceName: "New Name",
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.admin.updateSettings",
		bytes.NewReader(body),
	)
	req.AddCookie(sessionCookie(t, store))
	rec := httptest.NewRecorder()
	server.UpdateSettings(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var out habitat.NetworkHabitatAdminUpdateSettingsOutput
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	require.Equal(t, "New Name", out.InstanceName)
	require.Equal(t, "invite_only", out.OrgCreationPolicy)
}

func TestIssueInvite_ReturnsToken(t *testing.T) {
	server, store, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/xrpc/network.habitat.admin.issueInvite", nil)
	req.AddCookie(sessionCookie(t, store))
	rec := httptest.NewRecorder()
	server.IssueInvite(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var out habitat.NetworkHabitatAdminIssueInviteOutput
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	require.NotEmpty(t, out.Token)
}

func TestDescribeInstance_NoAuthRequired(t *testing.T) {
	server, store, _ := newTestServer(t)
	require.NoError(t, store.UpdateSettings(t.Context(), "Acme Hosting", "invite_only"))

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.instance.describeInstance",
		nil,
	)
	rec := httptest.NewRecorder()
	server.DescribeInstance(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var out habitat.NetworkHabitatInstanceDescribeInstanceOutput
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	require.Equal(t, "Acme Hosting", out.Name)
	require.True(t, out.InviteRequired)
}
