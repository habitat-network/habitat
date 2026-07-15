package sap

import (
	"context"
	"fmt"
	"sync"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"golang.org/x/sync/singleflight"
)

type sessionGetter struct {
	oauthClient *oauth.ClientApp
	sessions    sync.Map
	sf          *singleflight.Group
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

func key(did syntax.DID, sessionID string) string {
	return fmt.Sprintf("%s+%s", did.String(), sessionID)
}
