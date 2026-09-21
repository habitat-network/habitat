package oauthserver

import (
	"context"
	"net"
	"net/url"
	"slices"
)

// normalizeLoopbackRedirect rewrites a request's http://localhost[:port]
// redirect_uri to http://127.0.0.1[:port] when the client didn't register that
// exact URI. fosite only applies RFC 8252's ignore-the-port rule to loopback IP
// literals, not the name "localhost", but native clients such as Claude Code
// register http://localhost/callback and http://127.0.0.1/callback and then
// request localhost with an ephemeral port. Applying the same rewrite in the
// authorize, PAR and token handlers keeps the stored and token-time
// redirect_uri equal, which fosite requires.
func (o *OAuthServer) normalizeLoopbackRedirect(ctx context.Context, form url.Values) {
	clientID, raw := form.Get("client_id"), form.Get("redirect_uri")
	if clientID == "" || raw == "" {
		return
	}
	redirect, err := url.Parse(raw)
	if err != nil || redirect.Scheme != "http" || redirect.Hostname() != "localhost" {
		return
	}
	c, err := o.storage.GetClient(ctx, clientID)
	if err != nil || slices.Contains(c.GetRedirectURIs(), raw) {
		return
	}
	if port := redirect.Port(); port != "" {
		redirect.Host = net.JoinHostPort("127.0.0.1", port)
	} else {
		redirect.Host = "127.0.0.1"
	}
	form.Set("redirect_uri", redirect.String())
}
