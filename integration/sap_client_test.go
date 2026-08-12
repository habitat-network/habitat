package integration

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/habitat-network/habitat/api/habitat"
)

// pearHost is the canonical host pear is configured with (HABITAT_DOMAIN).
// Requests go to pear's published port directly, so the Host header has to be
// set explicitly for pear's host-aware routing to recognize them.
const pearHost = "pear.local.habitat.network"

// grantJWTBearer is the RFC 7523 JWT-bearer grant type, which pear accepts
// from confidential clients to mint act-as-subject access tokens.
const grantJWTBearer = "urn:ietf:params:oauth:grant-type:jwt-bearer"

// pearClient drives pear's XRPC API as an atproto confidential OAuth client.
// For each subject DID it mints an act-as-subject access token via the
// JWT-bearer grant (signing an ES256 assertion with the key published in its
// client metadata) and caches it for subsequent calls.
type pearClient struct {
	httpBase string // published HTTP endpoint, e.g. http://127.0.0.1:32768
	issuer   string // canonical https base, used for the assertion audience
	clientID string
	key      *ecdsa.PrivateKey
	http     *http.Client

	mu     sync.Mutex
	tokens map[string]string
}

func newPearClient(httpBase, issuer, clientID string, key *ecdsa.PrivateKey) *pearClient {
	return &pearClient{
		httpBase: strings.TrimSuffix(httpBase, "/"),
		issuer:   strings.TrimSuffix(issuer, "/"),
		clientID: clientID,
		key:      key,
		http:     &http.Client{Timeout: 30 * time.Second},
		tokens:   map[string]string{},
	}
}

// token returns a cached or freshly minted access token authorizing the
// client to act as subject.
func (c *pearClient) token(ctx context.Context, subject string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if tok, ok := c.tokens[subject]; ok {
		return tok, nil
	}

	now := time.Now()
	assertionTok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": c.clientID,
		"sub": subject,
		"aud": c.issuer + "/oauth/token",
		"iat": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
		"jti": randJTI(),
	})
	assertionTok.Header["kid"] = "test-client"
	assertion, err := assertionTok.SignedString(c.key)
	if err != nil {
		return "", fmt.Errorf("sign assertion for %s: %w", subject, err)
	}

	form := url.Values{"grant_type": {grantJWTBearer}, "assertion": {assertion}}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.httpBase+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Host = pearHost
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("mint token for %s: %w", subject, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("mint token for %s: status %d: %s", subject, resp.StatusCode, body)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode token for %s: %w", subject, err)
	}
	c.tokens[subject] = out.AccessToken
	return out.AccessToken, nil
}

// xrpc calls an XRPC method as subject, POSTing in as JSON (or GETting when
// in is nil) and decoding the response into out (skipped when out is nil).
// An empty subject sends the request unauthenticated.
func (c *pearClient) xrpc(ctx context.Context, method, subject string, in, out any) error {
	var body io.Reader
	httpMethod := http.MethodGet
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
		httpMethod = http.MethodPost
	}

	req, err := http.NewRequestWithContext(ctx, httpMethod, c.httpBase+"/xrpc/"+method, body)
	if err != nil {
		return err
	}
	req.Host = pearHost
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Habitat-Auth-Method", "oauth")
	if subject != "" {
		tok, err := c.token(ctx, subject)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: status %d: %s", method, resp.StatusCode, b)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// createOrg creates an organization with a password-login admin. Org creation
// is unauthenticated on an instance with the default open creation policy.
func (c *pearClient) createOrg(
	ctx context.Context,
	name, adminHandle string,
) (habitat.NetworkHabitatOrgCreateOutput, error) {
	var out habitat.NetworkHabitatOrgCreateOutput
	err := c.xrpc(ctx, "network.habitat.org.create", "", habitat.NetworkHabitatOrgCreateInput{
		Name:          name,
		AdminHandle:   adminHandle,
		LoginMethod:   "password",
		AdminPassword: "password1234",
		ContactEmail:  "test@example.com",
	}, &out)
	return out, err
}

// issueInvite has the org admin mint a reusable member invite token.
func (c *pearClient) issueInvite(ctx context.Context, adminDID string) (string, error) {
	var out habitat.NetworkHabitatOrgIssueInviteTokenOutput
	err := c.xrpc(ctx, "network.habitat.org.issueInviteToken", adminDID,
		habitat.NetworkHabitatOrgIssueInviteTokenInput{Reusable: true}, &out)
	return out.Token, err
}

// mintMember redeems an invite token for a new org member identity and
// returns its DID.
func (c *pearClient) mintMember(ctx context.Context, orgID, token, handle string) (string, error) {
	var out habitat.NetworkHabitatOrgMintMemberIdentityOutput
	err := c.xrpc(ctx, "network.habitat.org.mintMemberIdentity", "",
		habitat.NetworkHabitatOrgMintMemberIdentityInput{
			OrgId: orgID, Token: token, Handle: handle,
		}, &out)
	return out.Did, err
}

// createSpace creates a space authored by member (owned by member's org) and
// returns its space URI.
func (c *pearClient) createSpace(
	ctx context.Context,
	member, spaceType, skey string,
) (string, error) {
	var out habitat.NetworkHabitatSimplespaceCreateSpaceOutput
	err := c.xrpc(ctx, "network.habitat.simplespace.createSpace", member,
		habitat.NetworkHabitatSimplespaceCreateSpaceInput{Type: spaceType, Skey: skey}, &out)
	return out.Uri, err
}

// putRecord writes a record into member's repo within space and returns its
// space record URI.
func (c *pearClient) putRecord(
	ctx context.Context,
	member, space, collection, rkey string,
	record map[string]any,
) (string, error) {
	var out habitat.NetworkHabitatSpacePutRecordOutput
	err := c.xrpc(ctx, "network.habitat.space.putRecord", member,
		habitat.NetworkHabitatSpacePutRecordInput{
			Space:      space,
			Repo:       member,
			Collection: collection,
			Rkey:       rkey,
			Record:     record,
		}, &out)
	return out.Uri, err
}

// deleteRecord deletes member's record at rkey within space.
func (c *pearClient) deleteRecord(
	ctx context.Context,
	member, space, collection, rkey string,
) error {
	return c.xrpc(ctx, "network.habitat.space.deleteRecord", member,
		habitat.NetworkHabitatSpaceDeleteRecordInput{
			Space:      space,
			Repo:       member,
			Collection: collection,
			Rkey:       rkey,
		}, nil)
}

// randJTI generates a random JWT ID, so replay protection on the token
// endpoint doesn't reject repeated assertions.
func randJTI() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
