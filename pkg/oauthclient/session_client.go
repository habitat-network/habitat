package oauthclient

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// SessionClient wraps a resumed OAuth session as a plain *http.Client that
// authenticates every request with the session's DPoP-bound access token.
// Path-only request URLs are resolved against the session's host, and the
// service-auth lxm DoWithAuth needs is derived from the /xrpc/<nsid> path.
func SessionClient(sess *oauth.ClientSession) *http.Client {
	return &http.Client{Transport: &sessionTransport{sess: sess}}
}

type sessionTransport struct {
	sess *oauth.ClientSession
}

func (t *sessionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !req.URL.IsAbs() {
		base, err := url.Parse(t.sess.Data.HostURL)
		if err != nil {
			return nil, fmt.Errorf("parse session host url: %w", err)
		}
		req.URL.Scheme = base.Scheme
		req.URL.Host = base.Host
	}
	lxm, err := syntax.ParseNSID(strings.TrimPrefix(req.URL.Path, "/xrpc/"))
	if err != nil {
		return nil, fmt.Errorf("derive lxm from path %q: %w", req.URL.Path, err)
	}
	return t.sess.DoWithAuth(t.sess.Client, req, lxm)
}
