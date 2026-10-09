package login

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/workos/workos-go/v6/pkg/usermanagement"
)

// workosProvider signs users in with WorkOS AuthKit. WorkOS owns the
// credentials and sessions, so nothing is persisted here. Exchange reports
// the user's verified email as the login ID and the WorkOS organizations
// they have an active membership in via Profile.ExternalOrgs; deciding
// which habitat org that maps to is the caller's job.
type workosProvider struct {
	client      *usermanagement.Client
	clientID    string
	redirectURL string
}

type workosProviderState struct {
	State string `json:"state"`
}

// NewWorkOSProvider returns a Provider for WorkOS AuthKit. apiKey is the
// WorkOS secret API key; it authenticates both the code exchange and the
// membership lookup. baseURL overrides the WorkOS API endpoint (for tests);
// pass "" for production.
func NewWorkOSProvider(
	clientID, apiKey, redirectURL, baseURL string,
) (Provider, error) {
	if clientID == "" || apiKey == "" {
		return nil, fmt.Errorf("workos client id and api key are required")
	}
	client := usermanagement.NewClient(apiKey)
	if baseURL != "" {
		client.Endpoint = strings.TrimRight(baseURL, "/")
	}
	return &workosProvider{client: client, clientID: clientID, redirectURL: redirectURL}, nil
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
	authURL, err := p.client.GetAuthorizationURL(usermanagement.GetAuthorizationURLOpts{
		ClientID:    p.clientID,
		RedirectURI: p.redirectURL,
		Provider:    "authkit",
		State:       state,
		LoginHint:   loginHint,
	})
	if err != nil {
		return "", "", nil, fmt.Errorf("workos authorization url: %w", err)
	}
	return authURL.String(), state, stateBytes, nil
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

	auth, err := p.client.AuthenticateWithCode(ctx, usermanagement.AuthenticateWithCodeOpts{
		ClientID: p.clientID,
		Code:     code,
	})
	if err != nil {
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
	var orgs []ExternalOrg
	opts := usermanagement.ListOrganizationMembershipsOpts{
		UserID:   auth.User.ID,
		Statuses: []usermanagement.OrganizationMembershipStatus{usermanagement.Active},
		Limit:    100,
	}
	for {
		page, err := p.client.ListOrganizationMemberships(ctx, opts)
		if err != nil {
			return "", Profile{}, fmt.Errorf("workos list memberships: %w", err)
		}
		for _, m := range page.Data {
			if m.Status != usermanagement.Active {
				continue
			}
			orgs = append(orgs, ExternalOrg{ID: m.OrganizationID, Name: m.OrganizationName})
		}
		if page.ListMetadata.After == "" {
			break
		}
		opts.After = page.ListMetadata.After
	}

	return auth.User.Email, Profile{
		Name:         strings.TrimSpace(auth.User.FirstName + " " + auth.User.LastName),
		Picture:      auth.User.ProfilePictureURL,
		ExternalOrgs: orgs,
	}, nil
}
