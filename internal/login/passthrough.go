package login

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
)

// PassthroughHandleSuffix is the handle suffix the Router signs in through the
// passthrough provider. ".test" is reserved by RFC 2606 and never resolves on
// the public internet, so such handles only exist in dev and test setups.
const PassthroughHandleSuffix = ".test"

// passthroughProvider performs no authentication: Authorize redirects straight
// back to the callback, and Exchange accepts whatever comes back, reporting the
// login hint as the login id. It must only ever be wired up for dev/test.
type passthroughProvider struct {
	callbackURL string
}

// NewPassthroughProvider returns a Provider that skips authentication and
// immediately redirects to callbackURL (pear's /oauth-callback).
func NewPassthroughProvider(callbackURL string) Provider {
	return &passthroughProvider{callbackURL: callbackURL}
}

func (p *passthroughProvider) Authorize(
	_ context.Context,
	loginHint string,
) (string, string, string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", "", "", fmt.Errorf("generate state: %w", err)
	}
	state := hex.EncodeToString(nonce)
	u, err := url.Parse(p.callbackURL)
	if err != nil {
		return "", "", "", fmt.Errorf("parse callback url: %w", err)
	}
	q := u.Query()
	q.Set("state", state)
	// Carried through so Exchange can report who "signed in".
	q.Set("login_hint", loginHint)
	u.RawQuery = q.Encode()
	return u.String(), state, "", nil
}

func (p *passthroughProvider) Exchange(
	_ context.Context,
	query url.Values,
	_ string,
) (string, Profile, error) {
	return query.Get("login_hint"), Profile{}, nil
}
