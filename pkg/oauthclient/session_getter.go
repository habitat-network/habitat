package oauthclient

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

// SessionGetter resumes and caches OAuth client sessions. Concurrent calls to
// [SessionGetter.ResumeSession] for the same (DID, sessionID) are
// deduplicated via singleflight, and a resumed session is evicted from the
// cache once every handle referencing it has been released.
type SessionGetter struct {
	oauthClient *oauth.ClientApp
	sessions    sync.Map // cacheKey(did, sessionID) -> *cachedSession
	sf          singleflight.Group
}

// cachedSession is the shared, ref-counted resumed session kept in the cache.
// wg tracks every outstanding [Session] handle referencing it.
type cachedSession struct {
	*oauth.ClientSession
	wg sync.WaitGroup
}

// Session is a per-call handle on a resumed OAuth session, returned by
// [SessionGetter.ResumeSession]. Call [Session.Close] once done with it; the
// underlying session is evicted from the cache once every handle referencing
// it has been released, via Close or context cancellation, whichever comes
// first (each handle's release fires exactly once).
type Session struct {
	*oauth.ClientSession
	release func()
}

// NewSessionGetter creates a SessionGetter that resumes sessions through
// oauthClient.
func NewSessionGetter(oauthClient *oauth.ClientApp) *SessionGetter {
	return &SessionGetter{
		oauthClient: oauthClient,
	}
}

// ResumeSession resumes the session for (did, sessionID), reusing the cached
// session if another concurrent caller already resumed (or is resuming) it.
// Each call returns its own handle: release it via [Session.Close] once done,
// or let ctx being done release it automatically.
func (s *SessionGetter) ResumeSession(
	ctx context.Context,
	did syntax.DID,
	sessionID string,
) (*Session, error) {
	key := cacheKey(did, sessionID)
	cached, ok := s.sessions.Load(key)
	if !ok {
		resp, err, _ := s.sf.Do(key, func() (any, error) {
			// Another caller may have populated the cache while this call
			// was waiting to become the singleflight leader.
			if cached, ok := s.sessions.Load(key); ok {
				return cached, nil
			}
			clientSess, err := s.oauthClient.ResumeSession(ctx, did, sessionID)
			if err != nil {
				return nil, fmt.Errorf("resume session: %w", err)
			}
			cs := &cachedSession{ClientSession: clientSess}
			s.sessions.Store(key, cs)
			// Evict once every handle referencing this session is released.
			go func() {
				cs.wg.Wait()
				s.sessions.Delete(key)
			}()
			return cs, nil
		})
		if err != nil {
			return nil, fmt.Errorf("resume session: %w", err)
		}
		cached = resp
	}
	cs := cached.(*cachedSession)
	cs.wg.Add(1)
	var once sync.Once
	release := func() { once.Do(cs.wg.Done) }
	stop := context.AfterFunc(ctx, release)
	return &Session{
		ClientSession: cs.ClientSession,
		release: func() {
			stop()
			release()
		},
	}, nil
}

// Close releases this handle on the session. Safe to call more than once (a
// second call is a no-op) and safe to race with ctx being done.
func (s *Session) Close() {
	s.release()
}

// Do resumes the session for (did, sessionID) and performs req against it,
// releasing the session once the call returns. If req.URL has no host, it is
// resolved against the session's PDS host URL.
func (s *SessionGetter) Do(
	ctx context.Context,
	did syntax.DID,
	sessionID string,
	req *http.Request,
) (*http.Response, error) {
	session, err := s.ResumeSession(ctx, did, sessionID)
	if err != nil {
		return nil, fmt.Errorf("resume session: %w", err)
	}
	defer session.Close()

	if req.URL.Host == "" {
		hostURL, err := url.Parse(session.Data.HostURL)
		if err != nil {
			return nil, fmt.Errorf("parse host url: %w", err)
		}
		req.URL = hostURL.ResolveReference(req.URL)
	}
	nsidStr := strings.TrimPrefix(req.URL.Path, "/xrpc/")
	nsid, err := syntax.ParseNSID(nsidStr)
	if err != nil {
		return nil, fmt.Errorf("parse nsid: %w", err)
	}
	return session.DoWithAuth(http.DefaultClient, req, nsid)
}

// authTransport applies the session's OAuth DPoP auth to every request, for
// callers (like an SSE subscriber) that build requests through an
// *http.Client rather than calling DoWithAuth directly.
type authTransport struct {
	sess     *Session
	endpoint syntax.NSID
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.sess.DoWithAuth(t.sess.Client, req, t.endpoint)
}

// AuthClient returns an *http.Client that authenticates every request to
// endpoint with this session's OAuth tokens.
func (s *Session) AuthClient(endpoint syntax.NSID) *http.Client {
	return &http.Client{Transport: &authTransport{sess: s, endpoint: endpoint}}
}

func cacheKey(did syntax.DID, sessionID string) string {
	return fmt.Sprintf("%s+%s", did.String(), sessionID)
}
