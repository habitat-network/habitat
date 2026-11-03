package oauthserver

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ory/fosite"
)

const (
	// LoginHintCookieName is the cookie pear sets once a user signs in, naming
	// who they signed in as so neither pear nor sibling apps have to ask again.
	// It is deliberately readable by JavaScript (not HttpOnly): apps on other
	// subdomains read it to pass the user's DID straight to their OAuth client
	// instead of prompting for a handle. See typescript/internal's
	// rememberedLogin.ts for the reader.
	LoginHintCookieName = "habitat_login_hint"

	loginHintCookieMaxAge = 30 * 24 * time.Hour
	// loginHintKeyLabel derives the cookie's signing key from the OAuth server
	// secret, so the cookie needs no secret of its own and its MAC can't be
	// replayed as any other HMAC pear computes with that secret.
	loginHintKeyLabel = "habitat login hint cookie v1"
)

// CookieDomain is the Domain attribute pear scopes its login-hint cookie to,
// such as "habitat.network" for the cookie to reach every *.habitat.network
// app. Empty means a host-only cookie that only pear itself sees.
type CookieDomain string

// rememberedLogin is the payload of the login-hint cookie. It holds only
// public identifiers, never anything that grants access: it's a hint of who
// to sign in as, and every use of it still goes through the user's real login.
type rememberedLogin struct {
	DID syntax.DID `json:"did"`
	// Handle is for display (e.g. prefilling a sign-in form); empty when the
	// DID's handle couldn't be resolved or isn't valid.
	Handle   string `json:"handle,omitempty"`
	IssuedAt int64  `json:"iat"`
}

// loginHintCookie writes, reads and clears the signed login-hint cookie.
//
// The value is base64url(JSON payload) + "." + base64url(HMAC-SHA256 of the
// first part). JavaScript readers only decode the first part; the signature is
// for pear, which only trusts a hint it issued itself, so a sibling subdomain
// that can write a cookie on the shared parent domain can't plant one that
// silently steers a user's next pear sign-in to an account of its choosing.
type loginHintCookie struct {
	key    []byte
	domain CookieDomain
	now    func() time.Time
}

func newLoginHintCookie(
	secret []byte,
	domain CookieDomain,
	issuer string,
) (*loginHintCookie, error) {
	if domain != "" {
		// A browser drops a Set-Cookie whose Domain doesn't cover the host
		// that sent it, so catch the misconfiguration at startup rather than
		// with a cookie that silently never appears.
		u, err := url.Parse(issuer)
		if err != nil {
			return nil, fmt.Errorf("parse issuer: %w", err)
		}
		d := strings.ToLower(strings.TrimPrefix(string(domain), "."))
		host := strings.ToLower(u.Hostname())
		if host != d && !strings.HasSuffix(host, "."+d) {
			return nil, fmt.Errorf(
				"login hint cookie domain %q does not cover issuer host %q", domain, host,
			)
		}
		domain = CookieDomain(d)
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(loginHintKeyLabel))
	return &loginHintCookie{key: mac.Sum(nil), domain: domain, now: time.Now}, nil
}

func (c *loginHintCookie) sign(payload string) string {
	mac := hmac.New(sha256.New, c.key)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (c *loginHintCookie) cookie(value string, maxAge time.Duration) *http.Cookie {
	return &http.Cookie{
		Name:   LoginHintCookieName,
		Value:  value,
		Path:   "/",
		Domain: string(c.domain),
		MaxAge: int(maxAge.Seconds()),
		Secure: true,
		// Readable by JS on purpose; see LoginHintCookieName.
		HttpOnly: false,
		// Lax, not None: sibling subdomains are same-site, so they see it on
		// both navigations and fetches, while cross-site pages never get it.
		SameSite: http.SameSiteLaxMode,
	}
}

// set remembers did (and its handle, if dir resolves one) as the signed-in
// user. Handle resolution is best effort: a DID alone is still a usable hint.
func (c *loginHintCookie) set(
	ctx context.Context,
	w http.ResponseWriter,
	dir identity.Directory,
	did syntax.DID,
) {
	login := rememberedLogin{DID: did, IssuedAt: c.now().Unix()}
	if dir != nil {
		if id, err := dir.LookupDID(ctx, did); err == nil && !id.Handle.IsInvalidHandle() {
			login.Handle = id.Handle.String()
		}
	}
	b, err := json.Marshal(login)
	if err != nil {
		// Marshaling a struct of strings can't fail; nothing to remember if it did.
		return
	}
	payload := base64.RawURLEncoding.EncodeToString(b)
	http.SetCookie(w, c.cookie(payload+"."+c.sign(payload), loginHintCookieMaxAge))
}

// read returns the login remembered in r's login-hint cookie, if r carries one
// pear signed that hasn't expired.
func (c *loginHintCookie) read(r *http.Request) (rememberedLogin, bool) {
	ck, err := r.Cookie(LoginHintCookieName)
	if err != nil {
		return rememberedLogin{}, false
	}
	payload, sig, ok := strings.Cut(ck.Value, ".")
	if !ok || !hmac.Equal([]byte(sig), []byte(c.sign(payload))) {
		return rememberedLogin{}, false
	}
	b, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return rememberedLogin{}, false
	}
	var login rememberedLogin
	if err := json.Unmarshal(b, &login); err != nil {
		return rememberedLogin{}, false
	}
	if _, err := syntax.ParseDID(login.DID.String()); err != nil {
		return rememberedLogin{}, false
	}
	if c.now().Sub(time.Unix(login.IssuedAt, 0)) > loginHintCookieMaxAge {
		return rememberedLogin{}, false
	}
	return login, true
}

// clear expires the login-hint cookie.
func (c *loginHintCookie) clear(w http.ResponseWriter) {
	ck := c.cookie("", 0)
	ck.MaxAge = -1
	http.SetCookie(w, ck)
}

// asksForAccount reports whether the authorize request asked the user to pick
// or re-enter their account (OIDC prompt=login or prompt=select_account), in
// which case a remembered login must not be used to skip that step.
func asksForAccount(requester fosite.AuthorizeRequester) bool {
	for p := range strings.FieldsSeq(requester.GetRequestForm().Get("prompt")) {
		if p == "login" || p == "select_account" {
			return true
		}
	}
	return false
}

// HandleForgetLogin clears the login-hint cookie, for "sign out" / "not you?"
// flows that go through pear. Apps on the cookie's domain can also clear it
// themselves from JavaScript, since it isn't HttpOnly.
func (o *OAuthServer) HandleForgetLogin(w http.ResponseWriter, r *http.Request) {
	o.loginHint.clear(w)
	w.WriteHeader(http.StatusNoContent)
}
