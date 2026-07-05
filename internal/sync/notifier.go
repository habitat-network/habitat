package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/atproto"
	"github.com/habitat-network/habitat/internal/events"
	"github.com/habitat-network/habitat/internal/hive"
)

const notifyWriteEndpoint = syntax.NSID("com.atproto.space.notifyWrite")

type Notifier struct {
	eventStore events.EventStream
	hive       hive.Hive
	// TODO: this will blast notifications for all orgs to all subscribers
	// we should add a subscriber store that filters by org and scopes
	subscriberURLs []string
}

func NewNotifier(
	subscriberURLs []string,
	eventStore events.EventStream,
	hive hive.Hive,
) *Notifier {
	return &Notifier{
		eventStore:     eventStore,
		subscriberURLs: subscriberURLs,
		hive:           hive,
	}
}

func (n *Notifier) Start(ctx context.Context, event events.Event) error {
	ch := n.eventStore.Subscribe(ctx, 0)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case e := <-ch:
			for _, subscriberURL := range n.subscriberURLs {
				orgDID := e.Space.SpaceOwner()
				jwt, err := n.hive.SignServiceAuth(ctx,
					orgDID,
					subscriberURL,
					60*time.Second,
					new(notifyWriteEndpoint),
				)
				if err != nil {
					slog.ErrorContext(
						ctx,
						"failed to sign service auth",
						"err",
						err,
						"org",
						orgDID,
						"subscriber",
						subscriberURL,
					)
				}
				body, err := json.Marshal(atproto.ComAtprotoSpaceNotifyWriteInput{
					Space: e.Space.String(),
					Repo:  e.Repo.String(),
					Rev:   e.Rev.String(),
					Hash:  "TODO",
				})
				if err != nil {
					slog.ErrorContext(
						ctx,
						"failed to marshal request body",
						"err",
						err,
						"org",
						orgDID,
						"subscriber",
						subscriberURL,
					)
				}
				req, err := http.NewRequestWithContext(
					ctx,
					http.MethodPost,
					fmt.Sprintf("%s/xrpc/%s", subscriberURL, notifyWriteEndpoint),
					bytes.NewReader(body),
				)
				if err != nil {
					slog.ErrorContext(
						ctx,
						"failed to create request",
						"err",
						err,
						"org",
						orgDID,
						"subscriber",
						subscriberURL,
					)
				}
				req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", jwt))
				_, err = http.DefaultClient.Do(req)
				if err != nil {
					slog.ErrorContext(
						ctx,
						"failed to notify subscriber",
						"err",
						err,
						"org",
						orgDID,
						"subscriber",
						subscriberURL,
					)
				}
			}
		}
	}
}
