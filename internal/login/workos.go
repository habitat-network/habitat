package login

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const workosDefaultBaseURL = "https://api.workos.com"

// workosProvider signs users in with WorkOS AuthKit. WorkOS owns the
// credentials and sessions, so nothing is persisted here. Exchange reports
// the user's verified email as the login ID and the WorkOS organizations
// they have an active membership in via Profile.ExternalOrgIDs; deciding
// which habitat org that maps to is the caller's job.
type workosProvider struct {
	clientID    string
	apiKey      string
	redirectURL string
	baseURL     string
	httpClient  *http.Client
}

type workosProviderState struct {
	State string `json:"state"`
}

// NewWorkOSProvider returns a Provider for WorkOS AuthKit. apiKey is the
// WorkOS secret API key; it authenticates both the code exchange and the
// membership lookup. baseURL and httpClient are overridable for tests; pass
// "" and nil for production defaults.
func NewWorkOSProvider(
	clientID, apiKey, redirectURL, baseURL string,
	httpClient *http.Client,
) (Provider, error) {
	if clientID == "" || apiKey == "" {
		return nil, fmt.Errorf("workos client id and api key are required")
	}
	if baseURL == "" {
		baseURL = workosDefaultBaseURL
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &workosProvider{
		clientID:    clientID,
		apiKey:      apiKey,
		redirectURL: redirectURL,
		baseURL:     strings.TrimRight(baseURL, "/"),
		httpClient:  httpClient,
	}, nil
}

func (p *workosProvider) Authorize(
	ctx context.Context,
	loginHint string,
) (string, string, []byte, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", "", nil, fmt.Errorf("generate state: %w", err)
	}
	state := hex.EncodeToString(raw)
	stateBytes, err := json.Marshal(workosProviderState{State: state})
	if err != nil {
		return "", "", nil, fmt.Errorf("marshal workos state: %w", err)
	}
	q := url.Values{
		"client_id":     {p.clientID},
		"redirect_uri":  {p.redirectURL},
		"response_type": {"code"},
		"provider":      {"authkit"},
		"state":         {state},
	}
	if loginHint != "" {
		q.Set("login_hint", loginHint)
	}
	return p.baseURL + "/user_management/authorize?" + q.Encode(), state, stateBytes, nil
}

type workosUser struct {
	ID             string `json:"id"`
	Email          string `json:"email"`
	EmailVerified  bool   `json:"email_verified"`
	FirstName      string `json:"first_name"`
	LastName       string `json:"last_name"`
	ProfilePicture string `json:"profile_picture_url"`
}

type workosAuthResponse struct {
	User workosUser `json:"user"`
}

type workosMembershipsResponse struct {
	Data []struct {
		OrganizationID string `json:"organization_id"`
		Status         string `json:"status"`
	} `json:"data"`
}

func (p *workosProvider) Exchange(
	ctx context.Context,
	query url.Values,
	stateBytes []byte,
) (string, Profile, error) {
	var s workosProviderState
	if err := json.Unmarshal(stateBytes, &s); err != nil {
		return "", Profile{}, fmt.Errorf("unmarshal workos state: %w", err)
	}
	if s.State == "" || s.State != query.Get("state") {
		return "", Profile{}, fmt.Errorf("workos state mismatch")
	}
	code := query.Get("code")
	if code == "" {
		return "", Profile{}, fmt.Errorf("no code in workos callback")
	}

	var auth workosAuthResponse
	if err := p.do(ctx, http.MethodPost, "/user_management/authenticate", map[string]string{
		"client_id":     p.clientID,
		"client_secret": p.apiKey,
		"grant_type":    "authorization_code",
		"code":          code,
	}, &auth); err != nil {
		return "", Profile{}, fmt.Errorf("workos authenticate: %w", err)
	}
	if auth.User.Email == "" {
		return "", Profile{}, fmt.Errorf("no email in workos user")
	}
	if !auth.User.EmailVerified {
		return "", Profile{}, fmt.Errorf("workos email not verified")
	}

	// The authenticate response only names the organization chosen for this
	// session, so list every active membership to find all the user's orgs.
	var memberships workosMembershipsResponse
	if err := p.do(ctx, http.MethodGet, "/user_management/organization_memberships", url.Values{
		"user_id":  {auth.User.ID},
		"statuses": {"active"},
		"limit":    {"100"},
	}, &memberships); err != nil {
		return "", Profile{}, fmt.Errorf("workos list memberships: %w", err)
	}
	var orgIDs []string
	for _, m := range memberships.Data {
		if m.Status == "active" {
			orgIDs = append(orgIDs, m.OrganizationID)
		}
	}

	return auth.User.Email, Profile{
		Name:           strings.TrimSpace(auth.User.FirstName + " " + auth.User.LastName),
		Picture:        auth.User.ProfilePicture,
		ExternalOrgIDs: orgIDs,
	}, nil
}

// do calls the WorkOS API: POSTs send params (a map) as a JSON body, GETs
// send params (url.Values) as the query string. Requests carry the API key as a bearer token.
func (p *workosProvider) do(
	ctx context.Context,
	method, path string,
	params any,
	out any,
) error {
	var body io.Reader
	target := p.baseURL + path
	if q, ok := params.(url.Values); ok {
		target += "?" + q.Encode()
	} else {
		b, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
