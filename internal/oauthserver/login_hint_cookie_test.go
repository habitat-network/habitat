package oauthserver

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	dbtestutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/encrypt"
	keyval_testutil "github.com/habitat-network/habitat/internal/keyval/testutil"
	login_testutil "github.com/habitat-network/habitat/internal/login/testutil"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	"golang.org/x/oauth2"
)

func TestLoginHintCookieRoundTrip(t *testing.T) {
	c, err := newLoginHintCookie(
		[]byte("secret"),
		".Habitat.Example",
		"https://pear.habitat.example",
	)
	require.NoError(t, err)
	did := syntax.DID("did:web:example.did.com")

	w := httptest.NewRecorder()
	c.set(t.Context(), w, pdsclient.NewDummyDirectory("http://pds.url"), did)
	set := w.Result().Cookies()
	require.Len(t, set, 1)
	require.Equal(t, "habitat.example", set[0].Domain)
	require.True(t, set[0].Secure)
	require.False(t, set[0].HttpOnly, "apps must be able to read it from JS")
	require.Equal(t, http.SameSiteLaxMode, set[0].SameSite)

	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.AddCookie(set[0])
	got, ok := c.read(r)
	require.True(t, ok)
	require.Equal(t, did, got.DID)

	// The payload half is plain base64url JSON, which is all a JS reader decodes.
	payload, _, _ := strings.Cut(set[0].Value, ".")
	require.NotEmpty(t, payload)

	t.Run("rejects a forged payload", func(t *testing.T) {
		forged := rememberedLogin{DID: "did:web:attacker.example", IssuedAt: time.Now().Unix()}
		b, err := json.Marshal(forged)
		require.NoError(t, err)
		_, sig, _ := strings.Cut(set[0].Value, ".")
		r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		r.AddCookie(&http.Cookie{
			Name:  LoginHintCookieName,
			Value: base64.RawURLEncoding.EncodeToString(b) + "." + sig,
		})
		_, ok := c.read(r)
		require.False(t, ok)
	})

	t.Run("rejects a cookie signed with another secret", func(t *testing.T) {
		other, err := newLoginHintCookie([]byte("other"), "", "https://pear.habitat.example")
		require.NoError(t, err)
		r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		r.AddCookie(set[0])
		_, ok := other.read(r)
		require.False(t, ok)
	})

	t.Run("rejects an expired cookie", func(t *testing.T) {
		later := *c
		later.now = func() time.Time { return time.Now().Add(loginHintCookieMaxAge + time.Hour) }
		_, ok := later.read(r)
		require.False(t, ok)
	})

	t.Run("clear expires it on the same domain", func(t *testing.T) {
		w := httptest.NewRecorder()
		c.clear(w)
		cleared := w.Result().Cookies()
		require.Len(t, cleared, 1)
		require.Equal(t, "habitat.example", cleared[0].Domain)
		require.Negative(t, cleared[0].MaxAge)
	})
}

func TestLoginHintCookieRejectsDomainNotCoveringIssuer(t *testing.T) {
	_, err := newLoginHintCookie([]byte("secret"), "other.example", "https://pear.habitat.example")
	require.Error(t, err)
	// A suffix match must be on a label boundary.
	_, err = newLoginHintCookie([]byte("secret"), "tat.example", "https://pear.habitat.example")
	require.Error(t, err)
	_, err = newLoginHintCookie(
		[]byte("secret"),
		"pear.habitat.example",
		"https://pear.habitat.example",
	)
	require.NoError(t, err)
}

// TestOAuthServerRemembersLogin signs in once with a handle, then checks a
// second authorize request with no handle goes straight to the user's login
// instead of the handle prompt, unless the client asks for the prompt.
func TestOAuthServerRemembersLogin(t *testing.T) {
	db := dbtestutil.NewDB(t, Models())
	secretStr, err := encrypt.GenerateKey()
	require.NoError(t, err)
	secret, err := encrypt.ParseKey(secretStr)
	require.NoError(t, err)

	pds := login_testutil.NewPassthroughProvider(t)
	oauthServer, err := NewOAuthServer(
		secret,
		&org.LoginRouter{Pds: pds},
		pdsclient.NewDummyDirectory("http://pds.url"),
		db,
		keyval_testutil.New(t),
		noop.Meter{},
		testStore(t),
		"https://habitat.example",
		NewJWTBearerStore(),
		testOpensocialStore(t),
		nil,
		"",
	)
	require.NoError(t, err)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			oauthServer.HandleAuthorize(w, r)
		case "/oauth-callback":
			oauthServer.HandleCallback(w, r)
		case "/forget":
			oauthServer.HandleForgetLogin(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	pds.RedirectURI = server.URL + "/oauth-callback"

	clientApp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client-metadata.json" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(&pdsclient.ClientMetadata{
			ClientId:      "http://" + r.Host + "/client-metadata.json",
			RedirectUris:  []string{"http://" + r.Host + "/oauth-callback"},
			ResponseTypes: []string{"code"},
			GrantTypes:    []string{"authorization_code", "refresh_token"},
		}))
	}))
	defer clientApp.Close()
	config := &oauth2.Config{
		ClientID:    clientApp.URL + "/client-metadata.json",
		RedirectURL: clientApp.URL + "/oauth-callback",
		Endpoint:    oauth2.Endpoint{AuthURL: server.URL + "/authorize"},
	}
	authorizeURL := func(extra string) string {
		return config.AuthCodeURL(
			"test-state",
			oauth2.S256ChallengeOption(oauth2.GenerateVerifier()),
		) + extra
	}

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	client := server.Client()
	client.Jar = jar

	// First sign-in names the user, and follows redirects through the dummy
	// PDS back to the client app.
	resp, err := client.Get(authorizeURL("&handle=did:web:example.did.com"))
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	var remembered *http.Cookie
	for _, c := range jar.Cookies(serverURL) {
		if c.Name == LoginHintCookieName {
			remembered = c
		}
	}
	require.NotNil(t, remembered, "callback should remember the signed-in user")

	// From here on, stop at the authorize response to see where it sends us.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	firstHop := func(extra string) string {
		t.Helper()
		resp, err := client.Get(authorizeURL(extra))
		require.NoError(t, err)
		_ = resp.Body.Close()
		require.Equal(t, http.StatusSeeOther, resp.StatusCode)
		return resp.Header.Get("Location")
	}

	require.NotEqual(
		t,
		disambiguationPath,
		firstHop(""),
		"remembered login should skip the handle prompt",
	)
	require.Equal(t, disambiguationPath, firstHop("&prompt=select_account"))

	resp, err = client.Post(server.URL+"/forget", "", http.NoBody)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, disambiguationPath, firstHop(""), "forgotten login should prompt again")
}
