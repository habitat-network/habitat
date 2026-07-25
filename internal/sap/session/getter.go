package session

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"golang.org/x/sync/singleflight"
)

// getter caches resumed OAuth client sessions so concurrent component work
// for the same session shares one *oauth.ClientSession (and its DPoP/token
// refresh state) instead of resuming per request.
type getter struct {
	oauthClient *oauth.ClientApp
	sessions    sync.Map
	sf          singleflight.Group
}

// resumed is a cached client session. It is evicted from the cache once every
// context that resumed it has ended.
type resumed struct {
	*oauth.ClientSession
	wg sync.WaitGroup
}

func newGetter(oauthClient *oauth.ClientApp) *getter {
	return &getter{oauthClient: oauthClient}
}

// resume returns the cached session for (did, sessionID), resuming it through
// the OAuth client on first use. The session stays cached until every ctx
// passed to resume has ended.
func (g *getter) resume(
	ctx context.Context,
	did syntax.DID,
	sessionID string,
) (*resumed, error) {
	if sess, ok := g.sessions.Load(cacheKey(did, sessionID)); ok {
		return sess.(*resumed), nil
	}
	sessResp, err, _ := g.sf.Do(cacheKey(did, sessionID), func() (any, error) {
		clientSess, err := g.oauthClient.ResumeSession(ctx, did, sessionID)
		if err != nil {
			return nil, fmt.Errorf("resume session: %w", err)
		}
		sess := &resumed{ClientSession: clientSess}
		g.sessions.Store(cacheKey(did, sessionID), sess)
		return sess, nil
	})
	if err != nil {
		return nil, fmt.Errorf("resume session: %w", err)
	}
	sess := sessResp.(*resumed)
	sess.wg.Add(1)
	go func() {
		sess.wg.Wait()
		g.sessions.Delete(cacheKey(did, sessionID))
	}()
	go func() {
		<-ctx.Done()
		sess.wg.Done()
	}()
	return sess, nil
}

func cacheKey(did syntax.DID, sessionID string) string {
	return fmt.Sprintf("%s+%s", did.String(), sessionID)
}

// xrpcTransport authenticates every request with the session's OAuth DPoP
// auth, so components can keep using a plain *http.Client. Path-only request
// URLs are resolved against the session's Habitat host, and the service-auth
// lxm is derived from the /xrpc/<nsid> path.
type xrpcTransport struct {
	sess *resumed
}

func (t *xrpcTransport) RoundTrip(req *http.Request) (*http.Response, error) {
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

// authClient returns an *http.Client that authenticates every request with
// this session's OAuth tokens.
func (s *resumed) authClient() *http.Client {
	return &http.Client{Transport: &xrpcTransport{sess: s}}
}
