package sap

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"golang.org/x/sync/singleflight"
)

type sessionGetter struct {
	oauthClient *oauth.ClientApp
	sessions    sync.Map
	sf          singleflight.Group
}

type session struct {
	*oauth.ClientSession
	wg sync.WaitGroup
}

func newSessionGetter(oauthClient *oauth.ClientApp) *sessionGetter {
	return &sessionGetter{
		oauthClient: oauthClient,
	}
}

func (s *sessionGetter) ResumeSession(
	ctx context.Context,
	did syntax.DID,
	sessionID string,
) (*session, error) {
	if sess, ok := s.sessions.Load(key(did, sessionID)); ok {
		return sess.(*session), nil
	}
	sessResp, err, _ := s.sf.Do(key(did, sessionID), func() (any, error) {
		clientSess, err := s.oauthClient.ResumeSession(ctx, did, sessionID)
		if err != nil {
			return nil, fmt.Errorf("resume session: %w", err)
		}
		sess := &session{ClientSession: clientSess}
		s.sessions.Store(key(did, sessionID), sess)
		return sess, nil
	})
	if err != nil {
		return nil, fmt.Errorf("resume session: %w", err)
	}
	sess := sessResp.(*session)
	sess.wg.Add(1)
	go func() {
		sess.wg.Wait()
		s.sessions.Delete(key(did, sessionID))
	}()
	go func() {
		<-ctx.Done()
		sess.wg.Done()
	}()
	return sess, nil
}

func (s *session) Close() {
	s.wg.Done()
}

// authTransport applies the session's OAuth DPoP auth to every request, for
// callers (like the SSE subscriber) that build requests through an
// *http.Client rather than calling DoWithAuth directly.
type authTransport struct {
	sess     *session
	endpoint syntax.NSID
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.sess.DoWithAuth(t.sess.Client, req, t.endpoint)
}

// authClient returns an *http.Client that authenticates every request with
// this session's OAuth tokens.
func (s *session) authClient(endpoint syntax.NSID) *http.Client {
	return &http.Client{Transport: &authTransport{sess: s, endpoint: endpoint}}
}

func key(did syntax.DID, sessionID string) string {
	return fmt.Sprintf("%s+%s", did.String(), sessionID)
}
