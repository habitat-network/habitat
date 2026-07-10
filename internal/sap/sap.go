package sap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/habitat-network/habitat/internal/utils"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

type Sap struct {
	Outbox      Outbox
	oauthClient *oauthclient.App
	db          *gorm.DB
	sub         *subscriber
	resyncBuf   *resyncBuffer
	resyncer    *resyncer
	crawler     *crawler
	orgManager  *orgManager
	userManager *userManager
	metrics     *metrics
}

type SapConfig struct {
	DB                *gorm.DB
	ResyncParallelism int
	Directory         identity.Directory
	OAuthClient       *oauthclient.App
	Meter             metric.Meter
	Tracer            trace.Tracer
}

func NewSap(config SapConfig) (*Sap, error) {
	if err := autoMigrate(config.DB); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	m, err := newMetrics(config.Meter, config.Tracer)
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics: %w", err)
	}

	resyncNotif := utils.NewPollNotifier()
	outboxNotif := utils.NewPollNotifier()

	resyncBuf := newResyncBuffer(config.DB, resyncNotif, outboxNotif)
	sub := newSubscriber(config.DB, config.OAuthClient, resyncBuf, m)
	resyncer := newResyncer(
		config.DB,
		config.OAuthClient,
		resyncBuf,
		resyncNotif,
		outboxNotif,
		config.ResyncParallelism,
		m,
	)
	crawler := newCrawler(config.DB, config.OAuthClient, resyncBuf, sub, resyncNotif, m)
	outbox := newOutbox(config.DB, outboxNotif)
	orgManager := newOrgManager(config.DB)
	userManager := newUserManager(config.DB)

	return &Sap{
		orgManager:  orgManager,
		userManager: userManager,
		oauthClient: config.OAuthClient,
		Outbox:      outbox,
		db:          config.DB,
		sub:         sub,
		resyncBuf:   resyncBuf,
		resyncer:    resyncer,
		crawler:     crawler,
		metrics:     m,
	}, nil
}

func (s *Sap) Start(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return s.sub.loadSubscriptions(ctx)
	})

	eg.Go(func() error {
		return s.crawler.resumeIncompleteCrawls(ctx)
	})

	eg.Go(func() error {
		s.resyncer.run(ctx)
		return nil
	})

	err := eg.Wait()
	return errors.Join(err, s.sub.closeSubscriptions())
}

func (s *Sap) AddManagedOrg(ctx context.Context, did syntax.DID, sessionID string) error {
	org, err := s.orgManager.AddManagedOrg(ctx, did, sessionID)
	if err != nil {
		return err
	}
	go s.crawler.crawlOrg(detachSpan(ctx), org)
	go s.sub.addSubscription(detachSpan(ctx), org)
	return nil
}

func (s *Sap) ListManagedOrgs(ctx context.Context) ([]syntax.DID, error) {
	return s.orgManager.ListManagedOrgs(ctx)
}

// GetClient returns an HTTP client that authenticates as the given DID using
// the OAuth session sap tracks for it. The DID may be a managed org or an
// individual user (see StartUserLogin); managed orgs take precedence. Requests
// made with the returned client are resolved against the DID's Habitat (pear)
// host and carry its access token.
func (s *Sap) GetClient(ctx context.Context, did syntax.DID) (*http.Client, error) {
	org, err := s.orgManager.GetManagedOrg(ctx, did)
	if err == nil {
		return s.oauthClient.GetClient(ctx, did, org.SessionID)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	sess, err := s.userManager.GetUserSession(ctx, did)
	if err != nil {
		return nil, err
	}
	return s.oauthClient.GetClient(ctx, did, sess.SessionID)
}

// StartUserLogin begins an OAuth flow for an individual user (as opposed to a
// managed org). It records the flow keyed by the OAuth state so CompleteLogin
// can recognise it as a user login and redirect the browser back to
// redirectURL afterwards. Returns the PDS authorization URL to send the browser
// to.
func (s *Sap) StartUserLogin(ctx context.Context, handle, redirectURL string) (string, error) {
	authURL, err := s.oauthClient.StartAuthFlow(ctx, handle)
	if err != nil {
		return "", err
	}
	state, err := stateFromAuthURL(authURL)
	if err != nil {
		return "", err
	}
	if err := s.userManager.SaveLoginFlow(ctx, state, redirectURL); err != nil {
		return "", fmt.Errorf("save login flow: %w", err)
	}
	return authURL, nil
}

// CompleteLogin processes an OAuth callback. When the flow was started as a
// user login it stores the user's session and returns the redirect URL (with a
// login token appended) to send the browser back to. Otherwise it adds a
// managed org and returns an empty redirect, preserving the org-login
// behaviour.
func (s *Sap) CompleteLogin(ctx context.Context, params url.Values) (string, error) {
	sessionData, err := s.oauthClient.ProcessCallback(ctx, params)
	if err != nil {
		return "", fmt.Errorf("process callback: %w", err)
	}

	flow, isUser, err := s.userManager.GetLoginFlow(ctx, sessionData.SessionID)
	if err != nil {
		return "", fmt.Errorf("lookup login flow: %w", err)
	}
	if !isUser {
		if err := s.AddManagedOrg(ctx, sessionData.AccountDID, sessionData.SessionID); err != nil {
			return "", fmt.Errorf("add managed org: %w", err)
		}
		return "", nil
	}

	if err := s.userManager.AddUserSession(ctx, sessionData.AccountDID, sessionData.SessionID); err != nil {
		return "", fmt.Errorf("add user session: %w", err)
	}
	if err := s.userManager.CompleteLoginFlow(ctx, sessionData.SessionID, sessionData.AccountDID); err != nil {
		return "", fmt.Errorf("complete login flow: %w", err)
	}

	redirect, err := url.Parse(flow.RedirectURL)
	if err != nil {
		return "", fmt.Errorf("parse redirect url: %w", err)
	}
	q := redirect.Query()
	q.Set("login", sessionData.SessionID)
	redirect.RawQuery = q.Encode()
	return redirect.String(), nil
}

// GetCompletedLogin resolves a login token (handed to the redirect target by
// CompleteLogin) to the DID that authenticated, so the docs server can bind its
// server session to that user.
func (s *Sap) GetCompletedLogin(ctx context.Context, loginToken string) (syntax.DID, error) {
	return s.userManager.GetCompletedLogin(ctx, loginToken)
}

// stateFromAuthURL extracts the OAuth state query parameter from an
// authorization URL, which is the key both the auth-request and session stores
// use.
func stateFromAuthURL(authURL string) (string, error) {
	u, err := url.Parse(authURL)
	if err != nil {
		return "", fmt.Errorf("parse auth url: %w", err)
	}
	state := u.Query().Get("state")
	if state == "" {
		return "", fmt.Errorf("auth url missing state")
	}
	return state, nil
}
