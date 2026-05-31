package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type PearClient struct {
	baseURL     string
	accessToken string
	httpClient  *http.Client
}

func NewPearClient(baseURL, accessToken string) *PearClient {
	return &PearClient{
		baseURL:     baseURL,
		accessToken: accessToken,
		httpClient:  &http.Client{},
	}
}

func (c *PearClient) doGet(ctx context.Context, dest interface{}, path string, params url.Values) error {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	if params != nil {
		u.RawQuery = params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if c.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.accessToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pear error (status %d): %s", resp.StatusCode, string(body))
	}

	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("json decode: %w", err)
	}
	return nil
}

type listSpacesResponse struct {
	Spaces []map[string]interface{} `json:"spaces"`
}

func (c *PearClient) ListSpaces(ctx context.Context, spaceTypes []string) ([]map[string]interface{}, error) {
	params := url.Values{}
	for _, st := range spaceTypes {
		params.Add("spaceTypes", st)
	}
	var resp listSpacesResponse
	if err := c.doGet(ctx, &resp, "/xrpc/network.habitat.sync.listSpaces", params); err != nil {
		return nil, fmt.Errorf("listSpaces: %w", err)
	}
	return resp.Spaces, nil
}

func (c *PearClient) GetSpaceState(ctx context.Context, space string) (map[string]interface{}, error) {
	params := url.Values{"space": {space}}
	var state map[string]interface{}
	if err := c.doGet(ctx, &state, "/xrpc/network.habitat.sync.getSpaceState", params); err != nil {
		return nil, fmt.Errorf("getSpaceState: %w", err)
	}
	return state, nil
}

type listRecordsResponse struct {
	Records []map[string]interface{} `json:"records"`
	Cursor  string                   `json:"cursor"`
}

func (c *PearClient) ListRecords(ctx context.Context, space, repo, cursor string, limit int) ([]map[string]interface{}, string, error) {
	params := url.Values{"space": {space}}
	if repo != "" {
		params.Set("repo", repo)
	}
	if cursor != "" {
		params.Set("cursor", cursor)
	}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	var resp listRecordsResponse
	if err := c.doGet(ctx, &resp, "/xrpc/network.habitat.sync.listRecords", params); err != nil {
		return nil, "", fmt.Errorf("listRecords: %w", err)
	}
	return resp.Records, resp.Cursor, nil
}

type listRecordChangesResponse struct {
	Changes []map[string]interface{} `json:"changes"`
	Cursor  string                   `json:"cursor"`
}

func (c *PearClient) ListRecordChanges(ctx context.Context, space, repo, since string, limit int) ([]map[string]interface{}, string, error) {
	params := url.Values{"space": {space}}
	if repo != "" {
		params.Set("repo", repo)
	}
	if since != "" {
		params.Set("since", since)
	}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	var resp listRecordChangesResponse
	if err := c.doGet(ctx, &resp, "/xrpc/network.habitat.sync.listRecordChanges", params); err != nil {
		return nil, "", fmt.Errorf("listRecordChanges: %w", err)
	}
	return resp.Changes, resp.Cursor, nil
}

type getMemberOplogResponse struct {
	Ops    []map[string]interface{} `json:"ops"`
	Cursor string                   `json:"cursor"`
}

func (c *PearClient) GetMemberOplog(ctx context.Context, space, since string, limit int) ([]map[string]interface{}, string, error) {
	params := url.Values{"space": {space}}
	if since != "" {
		params.Set("since", since)
	}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	var resp getMemberOplogResponse
	if err := c.doGet(ctx, &resp, "/xrpc/network.habitat.sync.getMemberOplog", params); err != nil {
		return nil, "", fmt.Errorf("getMemberOplog: %w", err)
	}
	return resp.Ops, resp.Cursor, nil
}

func (c *PearClient) SubscribeSpaces(ctx context.Context, cursor int64, spaceTypes []string) (*SSEStream, error) {
	u, err := url.Parse(c.baseURL + "/xrpc/network.habitat.sync.subscribeSpaces")
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}
	q := u.Query()
	if cursor > 0 {
		q.Set("cursor", fmt.Sprintf("%d", cursor))
	}
	for _, st := range spaceTypes {
		q.Add("spaceTypes", st)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if c.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.accessToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("subscribe error (status %d): %s", resp.StatusCode, string(body))
	}

	return newSSEStream(resp.Body), nil
}

func getString(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func extractOrg(space string) string {
	if !strings.HasPrefix(space, "at://") {
		return space
	}
	parts := strings.SplitN(space[5:], "/", 2)
	return parts[0]
}
