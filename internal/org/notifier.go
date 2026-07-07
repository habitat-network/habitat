package org

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/core"
	"github.com/habitat-network/habitat/internal/hive"
)

// notifyAppNSID is the lexicon apps implement to be told about an org. It is
// used as the lexicon method (lxm) of the service-auth JWT.
var notifyAppNSID = syntax.NSID("network.habitat.org.notifyApp")

// Notifier tells habitat-compatible apps about the orgs on this instance by
// calling network.habitat.org.notifyApp on each builtin app. Apps use the
// notification to begin syncing the org's data.
type Notifier struct {
	// appURLs are the base URLs of the builtin apps to notify.
	appURLs []string
	hive    hive.Hive
	client  *http.Client
}

// NewNotifier returns a Notifier that notifies the given builtin apps.
func NewNotifier(appURLs []string, hve hive.Hive) *Notifier {
	return &Notifier{
		appURLs: appURLs,
		hive:    hve,
		client:  http.DefaultClient,
	}
}

// NotifyApps notifies every builtin app about the given org. It is best-effort
// and fire-and-forget: each app is notified in its own goroutine without waiting
// for the response, and a failure to notify one app is logged and ignored.
func (n *Notifier) NotifyApps(ctx context.Context, orgDID syntax.DID) {
	body, err := json.Marshal(habitat.NetworkHabitatOrgNotifyAppInput{
		Org: orgDID.String(),
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed to marshal notifyApp body", "err", err, "org", orgDID)
		return
	}
	// Detach from the caller's context so the in-flight requests aren't cancelled
	// when the caller returns.
	ctx = context.WithoutCancel(ctx)
	for _, appURL := range n.appURLs {
		go func() {
			if err := n.notifyApp(ctx, orgDID, appURL, body); err != nil {
				slog.ErrorContext(
					ctx,
					"failed to notify app",
					"err", err,
					"org", orgDID,
					"app", appURL,
				)
			}
		}()
	}
}

// notifyApp sends a single service-auth'd notifyApp request to appURL.
func (n *Notifier) notifyApp(
	ctx context.Context,
	orgDID syntax.DID,
	appURL string,
	body []byte,
) error {
	jwt, err := n.hive.SignServiceAuth(ctx, orgDID, appURL, 60*time.Second, &notifyAppNSID)
	if err != nil {
		return fmt.Errorf("sign service auth: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("%s/xrpc/%s", appURL, notifyAppNSID),
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", jwt))

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}
	return nil
}

type orgLister interface {
	ListOrgs(ctx context.Context) ([]core.Org, error)
}

// BootstrapNotifications notifies all builtin apps about every org registered
// on this instance. Intended to be called once at pear startup so apps can
// resume syncing existing orgs.
func BootstrapNotifications(ctx context.Context, notifier *Notifier, store orgLister) error {
	orgs, err := store.ListOrgs(ctx)
	if err != nil {
		return fmt.Errorf("list orgs: %w", err)
	}
	for _, o := range orgs {
		notifier.NotifyApps(ctx, o.DID())
	}
	return nil
}
