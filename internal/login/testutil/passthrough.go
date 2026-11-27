package testutil

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/habitat-network/habitat/internal/login"
	"github.com/habitat-network/habitat/internal/utils"
)

// PassthroughProvider implements [login.Provider] by starting an httptest server
// that simulates the PDS OAuth dance. When Authorize is called, it returns a
// redirect URL to the test server. The test server then redirects back to
// RedirectURI with a dummy code, allowing the full OAuth flow to complete
// in E2E tests.
type PassthroughProvider struct {
	LoginID     string
	RedirectURI string
	Server      *httptest.Server
}

// NewPassthroughProvider creates a PassthroughProvider with an httptest server
// that handles the OAuth redirect flow. The server redirects back to
// RedirectURI with a dummy code, enabling E2E tests to complete the full
// OAuth flow.
//
// Callers can optionally override LoginID or RedirectURI on the returned
// struct before any calls are made.
func NewPassthroughProvider(t *testing.T) *PassthroughProvider {
	t.Helper()
	p := &PassthroughProvider{}
	p.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := url.Parse(p.RedirectURI)
		if err != nil {
			http.Error(w, "invalid redirect URI", http.StatusInternalServerError)
			return
		}
		q := u.Query()
		q.Set("state", r.URL.Query().Get("state"))
		u.RawQuery = q.Encode()
		http.Redirect(w, r, u.String(), http.StatusSeeOther)
	}))
	t.Cleanup(p.Server.Close)
	return p
}

func (p *PassthroughProvider) Authorize(
	ctx context.Context,
	loginHint string,
) (redirectURI string, state string, providerStateKey string, err error) {
	if p.LoginID == "" {
		p.LoginID = loginHint
	}
	state = utils.RandomNonce(32)
	return p.Server.URL + "/authorize?state=" + url.QueryEscape(state), state, "", nil
}

func (p *PassthroughProvider) Exchange(
	ctx context.Context,
	query url.Values,
	providerStateKey string,
) (loginID string, profile login.Profile, err error) {
	return p.LoginID, login.Profile{}, nil
}

var _ login.Provider = (*PassthroughProvider)(nil)
