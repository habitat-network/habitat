package testutil

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/habitat-network/habitat/internal/login"
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
		http.Redirect(w, r, p.RedirectURI, http.StatusSeeOther)
	}))
	t.Cleanup(p.Server.Close)
	return p
}

func (p *PassthroughProvider) Authorize(
	ctx context.Context,
	loginHint string,
) (redirectURI string, state []byte, err error) {
	if p.LoginID == "" {
		p.LoginID = loginHint
	}
	return p.Server.URL + "/authorize", nil, nil
}

func (p *PassthroughProvider) Exchange(
	ctx context.Context,
	code string,
	issuer string,
	state []byte,
) (loginID string, err error) {
	return p.LoginID, nil
}

var _ login.Provider = (*PassthroughProvider)(nil)
