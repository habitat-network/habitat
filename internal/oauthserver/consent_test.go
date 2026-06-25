package oauthserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/login"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	"golang.org/x/oauth2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	orgConsentMemberDomain  = "unreachable.invalid"
	orgConsentPearDomain    = "pear." + orgConsentMemberDomain
	orgConsentAdminPassword = "correct-horse-battery-staple"
)

// orgConsentFixture bundles together a hive-served org (whose admin logs in
// with a password) and an OAuth server configured against it, for driving
// the org-DID authorize flow up to the admin consent screen.
type orgConsentFixture struct {
	passwordProvider *login.PasswordLoginProvider
	flowServer       *httptest.Server
	clientMetadata   *pdsclient.ClientMetadata
	oauthCfg         *oauth2.Config
	orgIdentity      *identity.Identity
	adminIdentity    *identity.Identity
}

// setupOrgConsentFixture wires up a hive-served org with a password-based
// admin login, an OAuth server, and a dummy OAuth client app.
func setupOrgConsentFixture(t *testing.T) *orgConsentFixture {
	t.Helper()

	// hive and the org store share one db: org.WithTx swaps hive's db
	// connection for its own transaction when minting member identities.
	hiveDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err, "failed to open hive database")
	h, err := hive.NewHive(orgConsentMemberDomain, orgConsentPearDomain, hiveDB)
	require.NoError(t, err, "failed to create hive")

	passwordProvider, err := login.NewPasswordProvider(
		hiveDB,
		orgConsentPearDomain,
		[]byte("test-signing-secret-for-org-consent"),
		h,
	)
	require.NoError(t, err, "failed to setup password provider")

	orgStore, err := org.NewStore(hiveDB, h, h, orgConsentPearDomain, passwordProvider)
	require.NoError(t, err, "failed to setup org store")

	orgIdentity, adminIdentity, err := orgStore.CreateOrg(
		t.Context(),
		"org-name",
		"admin",
		orgConsentAdminPassword,
		"password",
		"",
		"acme",
	)
	require.NoError(t, err, "failed to create org with password-based admin login")

	oauthDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err, "failed to open oauth database")
	secretStr, err := encrypt.GenerateKey()
	require.NoError(t, err)
	secret, err := encrypt.ParseKey(secretStr)
	require.NoError(t, err)

	oauthServer, err := NewOAuthServer(
		secret,
		&org.LoginRouter{Password: passwordProvider, OrgStore: orgStore},
		h, // the OAuth server resolves the org/admin handles via hive
		oauthDB,
		noop.Meter{},
		orgStore,
	)
	require.NoError(t, err, "failed to setup oauth server")

	flowServer := httptest.NewTLSServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/authorize":
				oauthServer.HandleAuthorize(w, r)
			case r.URL.Path == "/oauth-callback":
				oauthServer.HandleCallback(w, r)
			case r.URL.Path == "/token":
				oauthServer.HandleToken(w, r)
			case r.URL.Path == "/oauth/consent" && r.Method == http.MethodPost:
				oauthServer.HandleConsentDecision(w, r)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		},
	))
	t.Cleanup(flowServer.Close)

	clientMetadata := &pdsclient.ClientMetadata{
		ClientName:    "Acme Test Client",
		ResponseTypes: []string{"code"},
		GrantTypes:    []string{"authorization_code", "refresh_token"},
	}
	clientApp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/client-metadata.json":
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(clientMetadata))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(clientApp.Close)
	clientMetadata.ClientId = clientApp.URL + "/client-metadata.json"
	clientMetadata.RedirectUris = []string{clientApp.URL + "/oauth-callback"}

	oauthCfg := &oauth2.Config{
		Endpoint: oauth2.Endpoint{
			AuthURL:  flowServer.URL + "/authorize",
			TokenURL: flowServer.URL + "/token",
		},
		ClientID:    clientApp.URL + "/client-metadata.json",
		RedirectURL: clientApp.URL + "/oauth-callback",
	}

	return &orgConsentFixture{
		passwordProvider: passwordProvider,
		flowServer:       flowServer,
		clientMetadata:   clientMetadata,
		oauthCfg:         oauthCfg,
		orgIdentity:      orgIdentity,
		adminIdentity:    adminIdentity,
	}
}

// adminLogin simulates the admin submitting their handle/password to the
// loginMember XRPC procedure and returns the resulting one-time login code.
func (fx *orgConsentFixture) adminLogin(t *testing.T) string {
	t.Helper()
	body, err := json.Marshal(habitat.NetworkHabitatOrgLoginMemberInput{
		Handle:   fx.adminIdentity.Handle.String(),
		Password: orgConsentAdminPassword,
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	fx.passwordProvider.HandlePasswordLogin(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "admin login should succeed: %s", rec.Body)

	var out habitat.NetworkHabitatOrgLoginMemberOutput
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	callbackURL, err := url.Parse(out.CallbackURL)
	require.NoError(t, err)
	code := callbackURL.Query().Get("code")
	require.NotEmpty(t, code)
	return code
}

// driveToConsentPage runs the org-DID authorize flow, through the admin's
// login, up to and including the redirect to the consent page UI. It returns
// the HTTP client used (with cookies and redirect-stopping in place, so it can
// be reused to post the final decision) and the query params the callback
// passed to the consent page UI.
func (fx *orgConsentFixture) driveToConsentPage(t *testing.T) (*http.Client, url.Values) {
	t.Helper()

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	httpClient := &http.Client{
		Transport: fx.flowServer.Client().Transport,
		Jar:       jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	verifier := oauth2.GenerateVerifier()
	authReq, err := http.NewRequest(http.MethodGet, fx.oauthCfg.AuthCodeURL(
		"test-state",
		oauth2.S256ChallengeOption(verifier),
	)+"&handle="+fx.orgIdentity.Handle.String(), nil)
	require.NoError(t, err)
	resp, err := httpClient.Do(authReq)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusSeeOther, resp.StatusCode, "expected redirect to admin login page")

	code := fx.adminLogin(t)

	callbackReq, err := http.NewRequest(
		http.MethodGet,
		fx.flowServer.URL+"/oauth-callback?code="+url.QueryEscape(code),
		nil,
	)
	require.NoError(t, err)
	resp, err = httpClient.Do(callbackReq)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)

	loc, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	require.Equal(
		t,
		"/ui/oauth/consent",
		loc.Path,
		"org DID login should redirect to the consent page UI rather than issuing a code",
	)

	return httpClient, loc.Query()
}

// TestOAuthServerOrgDIDRequiresConsent drives the org-DID OAuth flow through
// admin login and asserts that an authorization code is only issued once the
// admin explicitly allows the request on the consent page, showing the
// client's metadata; denying yields an access_denied error instead.
func TestOAuthServerOrgDIDRequiresConsent(t *testing.T) {
	fx := setupOrgConsentFixture(t)

	t.Run("consent redirect carries client metadata and allow issues a code", func(t *testing.T) {
		httpClient, params := fx.driveToConsentPage(t)
		require.Equal(t, fx.clientMetadata.ClientName, params.Get("clientName"))
		require.Equal(t, fx.orgIdentity.Handle.String(), params.Get("orgHandle"))

		resp, err := httpClient.PostForm(
			fx.flowServer.URL+"/oauth/consent",
			url.Values{"decision": {"allow"}},
		)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, http.StatusSeeOther, resp.StatusCode)

		loc, err := url.Parse(resp.Header.Get("Location"))
		require.NoError(t, err)
		require.NotEmpty(
			t,
			loc.Query().Get("code"),
			"allow decision should issue an authorization code",
		)
	})

	t.Run("deny rejects the request", func(t *testing.T) {
		httpClient, _ := fx.driveToConsentPage(t)

		resp, err := httpClient.PostForm(
			fx.flowServer.URL+"/oauth/consent",
			url.Values{"decision": {"deny"}},
		)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, http.StatusSeeOther, resp.StatusCode)

		loc, err := url.Parse(resp.Header.Get("Location"))
		require.NoError(t, err)
		require.Equal(t, "access_denied", loc.Query().Get("error"))
	})
}
