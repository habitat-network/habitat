package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/events"
	"github.com/r3labs/sse/v2"
)

// pearClient talks to a pear instance on behalf of the search server: it
// authenticates the indexing stream with the instance-wide M2M token, and
// forwards a caller's own bearer token (unmodified) when resolving which org
// that caller belongs to.
type pearClient struct {
	host       string
	httpClient *http.Client
	m2mToken   string
}

func newPearClient(host, m2mToken string) *pearClient {
	return &pearClient{
		host:       host,
		httpClient: &http.Client{},
		m2mToken:   m2mToken,
	}
}

// SubscribeSpaces opens a long-lived SSE connection to pear's subscribeSpaces
// endpoint, authenticated with the M2M token, and invokes onEvent for every
// decoded "space" event starting from cursor (0 replays full history before
// tailing live events, per events.Store.Subscribe's behavior). Blocks until
// ctx is canceled or the connection fails.
func (c *pearClient) SubscribeSpaces(
	ctx context.Context,
	cursor uint64,
	onEvent func(events.Event),
) error {
	client := sse.NewClient(c.host + "/xrpc/network.habitat.sync.subscribeSpaces")
	client.Connection = c.httpClient
	client.Headers["Authorization"] = "Bearer " + c.m2mToken
	client.Headers["Habitat-Auth-Method"] = "oauth"
	if cursor > 0 {
		client.LastEventID.Store([]byte(strconv.FormatUint(cursor, 10)))
	}

	return client.SubscribeRawWithContext(ctx, func(msg *sse.Event) {
		if string(msg.Event) != "space" {
			return
		}
		var event events.Event
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			return
		}
		onEvent(event)
	})
}

// ResolveCallerOrg calls pear's network.habitat.org.getMetadata using the
// caller's own bearer token (forwarded as-is, not the M2M token) with no
// orgId param, which resolves to the caller's own org membership.
func (c *pearClient) ResolveCallerOrg(ctx context.Context, callerBearerToken string) (string, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, c.host+"/xrpc/network.habitat.org.getMetadata", nil,
	)
	if err != nil {
		return "", fmt.Errorf("build getMetadata request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+callerBearerToken)
	req.Header.Set("Habitat-Auth-Method", "oauth")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("call getMetadata: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("getMetadata returned status %d", resp.StatusCode)
	}
	var out habitat.NetworkHabitatOrgGetMetadataOutput
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode getMetadata response: %w", err)
	}
	return out.OrgId, nil
}
