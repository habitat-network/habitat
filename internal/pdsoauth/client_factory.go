package pdsoauth

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/httpx"
	lru "github.com/hashicorp/golang-lru/v2"
)

// HttpClient makes requests to a DID's PDS on that DID's behalf.
type HttpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// HttpClientFactory creates clients to make authenticated requests to a DID's
// atproto PDS.
type HttpClientFactory interface {
	NewClient(ctx context.Context, did syntax.DID) (HttpClient, error)
}

type clientFactory struct {
	app   *App
	store oauth.ClientAuthStore
	// sessions caches resumed sessions so concurrent requests for a DID share
	// one session, and so one token refresh: refresh tokens rotate, so racing
	// refreshes would invalidate each other.
	sessions *lru.Cache[syntax.DID, *oauth.ClientSession]
}

// NewHttpClientFactory returns a factory of clients for the sessions app has
// stored.
func NewHttpClientFactory(app *App) (HttpClientFactory, error) {
	cache, err := lru.New[syntax.DID, *oauth.ClientSession](1024 /* arbitrary size for now */)
	if err != nil {
		return nil, err
	}
	return &clientFactory{app: app, store: app.Store, sessions: cache}, nil
}

func (f *clientFactory) NewClient(ctx context.Context, did syntax.DID) (HttpClient, error) {
	// A sign-in replaces the DID's session under a new ID, which a cached
	// session must not outlive. Refreshes keep the ID.
	stored, err := f.store.GetSession(ctx, did, "")
	if err != nil {
		return nil, fmt.Errorf("[pds client factory]: get session: %w", err)
	}
	if sess, ok := f.sessions.Get(did); ok && sess.Data.SessionID == stored.SessionID {
		return &sessionClient{sess: sess}, nil
	}
	sess, err := f.app.ResumeSession(ctx, did, stored.SessionID)
	if err != nil {
		return nil, fmt.Errorf("[pds client factory]: resume session: %w", err)
	}
	f.sessions.Add(did, sess)
	return &sessionClient{sess: sess}, nil
}

// sessionClient sends requests with the session's access token, DPoP proof,
// nonce rotation and token refresh handled by indigo.
type sessionClient struct {
	sess *oauth.ClientSession
}

func (c *sessionClient) Do(req *http.Request) (*http.Response, error) {
	hostURL, err := url.Parse(c.sess.Data.HostURL)
	if err != nil {
		return nil, fmt.Errorf("parse pds url: %w", err)
	}
	req.URL = hostURL.ResolveReference(req.URL)
	// DoWithAuth may send the request twice (nonce update, token refresh), so
	// the body must be replayable.
	if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("read request body: %w", err)
		}
		_ = req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		}
	}
	// The NSID only labels metrics and logs in indigo, and callers here
	// forward arbitrary XRPC methods.
	return c.sess.DoWithAuth(httpx.NewClient(), req, "")
}
