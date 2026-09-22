package mcpoauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ory/fosite"
	"github.com/ory/fosite/handler/oauth2"
	"github.com/ory/fosite/handler/pkce"
	"gorm.io/gorm"
)

// requestTTL bounds how long an in-flight authorization request (waiting on the
// user's PDS login) or an issued authorization code stays usable.
const requestTTL = 10 * time.Minute

// Client is an OAuth client registered through RFC 7591 dynamic client
// registration. MCP clients are always public: they authenticate with PKCE.
type Client struct {
	ClientID     string `gorm:"primaryKey"`
	ClientName   string
	RedirectURIs []byte // JSON []string
	GrantTypes   []byte // JSON []string
	CreatedAt    time.Time
}

func (Client) TableName() string { return "mcp_oauth_clients" }

// Request is the single row type backing short-lived flow state: a pending
// authorization request (keyed by the atproto login's state token while the
// user is at their PDS, or by a form id before that) and issued authorization
// codes (keyed by code signature).
type Request struct {
	Key                 string    `gorm:"primaryKey"`
	ClientID            string    `gorm:"size:255"`
	Subject             string    `gorm:"size:255"`
	Scopes              string    `gorm:"size:512"`
	CodeChallenge       string    `gorm:"size:255"`
	CodeChallengeMethod string    `gorm:"size:32"`
	RedirectURI         string    `gorm:"size:1024"`
	State               string    `gorm:"size:1024"`
	ResponseType        string    `gorm:"size:64"`
	ResponseMode        string    `gorm:"size:32"`
	ExpiresAt           time.Time `gorm:"index"`
}

func (Request) TableName() string { return "mcp_oauth_requests" }

// RefreshSession backs refresh tokens. Access tokens are stateless JWTs.
type RefreshSession struct {
	Signature string `gorm:"primaryKey"`
	ClientID  string
	Subject   string
	Scopes    string
	ExpiresAt time.Time
}

func (RefreshSession) TableName() string { return "mcp_oauth_refresh_sessions" }

type store struct {
	db       *gorm.DB
	resource string
}

var (
	_ fosite.Storage                = (*store)(nil)
	_ oauth2.CoreStorage            = (*store)(nil)
	_ oauth2.TokenRevocationStorage = (*store)(nil)
	_ pkce.PKCERequestStorage       = (*store)(nil)
)

func newStore(db *gorm.DB, resource string) (*store, error) {
	if err := db.AutoMigrate(&Client{}, &Request{}, &RefreshSession{}); err != nil {
		return nil, fmt.Errorf("migrate mcp oauth tables: %w", err)
	}
	return &store{db: db, resource: resource}, nil
}

// toAuthorizeRequest rebuilds the fosite.AuthorizeRequest this row was stored
// from. The redirect URI is parsed because WriteAuthorizeResponse reads the
// struct field, not the form.
func (r *Request) toAuthorizeRequest(client fosite.Client, resource string) *fosite.AuthorizeRequest {
	scopes := fosite.Arguments(strings.Fields(r.Scopes))
	redirectURI, _ := url.Parse(r.RedirectURI)
	return &fosite.AuthorizeRequest{
		ResponseTypes:        fosite.Arguments(strings.Fields(r.ResponseType)),
		ResponseMode:         fosite.ResponseModeType(r.ResponseMode),
		HandledResponseTypes: fosite.Arguments{},
		RedirectURI:          redirectURI,
		State:                r.State,
		Request: fosite.Request{
			Client:         client,
			Session:        &session{Subject: r.Subject, ClientID: r.ClientID, Audience: resource, Scopes: scopes},
			RequestedScope: scopes,
			GrantedScope:   scopes,
			Form: url.Values{
				"code_challenge":        {r.CodeChallenge},
				"code_challenge_method": {r.CodeChallengeMethod},
			},
			RequestedAt: time.Now().UTC(),
		},
	}
}

func fromRequester(key string, requester fosite.Requester, expiresAt time.Time) *Request {
	form := requester.GetRequestForm()
	// A pending authorization request has no session yet: the subject is only
	// known once the user finishes the atproto login.
	var subject string
	if sess := requester.GetSession(); sess != nil {
		subject = sess.GetSubject()
	}
	return &Request{
		Key:                 key,
		ClientID:            requester.GetClient().GetID(),
		Subject:             subject,
		Scopes:              strings.Join(requester.GetRequestedScopes(), " "),
		CodeChallenge:       form.Get("code_challenge"),
		CodeChallengeMethod: form.Get("code_challenge_method"),
		RedirectURI:         form.Get("redirect_uri"),
		State:               form.Get("state"),
		ResponseType:        form.Get("response_type"),
		ResponseMode:        form.Get("response_mode"),
		ExpiresAt:           expiresAt,
	}
}

func (r *Request) pkceForm() url.Values {
	v := url.Values{}
	if r.CodeChallenge != "" {
		v.Set("code_challenge", r.CodeChallenge)
	}
	if r.CodeChallengeMethod != "" {
		v.Set("code_challenge_method", r.CodeChallengeMethod)
	}
	return v
}

// createPending persists an authorization request awaiting the user's login.
func (s *store) createPending(ctx context.Context, key string, ar fosite.AuthorizeRequester) error {
	return s.db.WithContext(ctx).Create(fromRequester(key, ar, time.Now().Add(requestTTL))).Error
}

// rekeyPending moves a pending request from its form id to the atproto login's
// state token, which is how the callback finds it again.
func (s *store) rekeyPending(ctx context.Context, from, to string) error {
	return s.db.WithContext(ctx).Model(&Request{}).Where("key = ?", from).Update("key", to).Error
}

func (s *store) getPending(ctx context.Context, key string) (*fosite.AuthorizeRequest, error) {
	return s.getRequest(ctx, key)
}

func (s *store) getRequest(ctx context.Context, key string) (*fosite.AuthorizeRequest, error) {
	var r Request
	err := s.db.WithContext(ctx).Where("expires_at > ?", time.Now()).First(&r, "key = ?", key).Error
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	client, err := s.GetClient(ctx, r.ClientID)
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	return r.toAuthorizeRequest(client, s.resource), nil
}

func (s *store) deletePending(ctx context.Context, key string) error {
	return s.db.WithContext(ctx).Delete(&Request{}, "key = ?", key).Error
}

func (s *store) createClient(ctx context.Context, c *Client) error {
	return s.db.WithContext(ctx).Create(c).Error
}

// GetClient implements fosite.Storage.
func (s *store) GetClient(ctx context.Context, id string) (fosite.Client, error) {
	var row Client
	if err := s.db.WithContext(ctx).First(&row, "client_id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.Join(fosite.ErrNotFound, err)
		}
		return nil, err
	}
	c := &fosite.DefaultClient{ID: row.ClientID, Public: true, Scopes: []string{scopeMCP}}
	if err := json.Unmarshal(row.RedirectURIs, &c.RedirectURIs); err != nil {
		return nil, fmt.Errorf("decode redirect uris: %w", err)
	}
	if err := json.Unmarshal(row.GrantTypes, &c.GrantTypes); err != nil {
		return nil, fmt.Errorf("decode grant types: %w", err)
	}
	c.ResponseTypes = []string{"code"}
	return c, nil
}

// ClientAssertionJWTValid implements fosite.Storage. Public clients only, so
// client assertions are never used.
func (s *store) ClientAssertionJWTValid(context.Context, string) error { return nil }

// SetClientAssertionJWT implements fosite.Storage.
func (s *store) SetClientAssertionJWT(context.Context, string, time.Time) error { return nil }

// CreateAuthorizeCodeSession implements oauth2.CoreStorage.
func (s *store) CreateAuthorizeCodeSession(
	ctx context.Context, signature string, requester fosite.Requester,
) error {
	return s.db.WithContext(ctx).Create(
		fromRequester(signature, requester, requester.GetSession().GetExpiresAt(fosite.AuthorizeCode)),
	).Error
}

// GetAuthorizeCodeSession implements oauth2.CoreStorage.
func (s *store) GetAuthorizeCodeSession(
	ctx context.Context, signature string, _ fosite.Session,
) (fosite.Requester, error) {
	return s.getRequest(ctx, signature)
}

// InvalidateAuthorizeCodeSession implements oauth2.CoreStorage.
func (s *store) InvalidateAuthorizeCodeSession(ctx context.Context, signature string) error {
	return s.deletePending(ctx, signature)
}

// CreatePKCERequestSession implements pkce.PKCERequestStorage. The challenge is
// written onto the authorize code's row, the one point in the flow where fosite
// hands it over un-sanitized.
func (s *store) CreatePKCERequestSession(
	ctx context.Context, signature string, requester fosite.Requester,
) error {
	form := requester.GetRequestForm()
	return s.db.WithContext(ctx).Model(&Request{}).Where("key = ?", signature).
		Updates(map[string]any{
			"code_challenge":        form.Get("code_challenge"),
			"code_challenge_method": form.Get("code_challenge_method"),
		}).Error
}

// DeletePKCERequestSession implements pkce.PKCERequestStorage. The challenge
// lives on the authorize code's row, which InvalidateAuthorizeCodeSession deletes.
func (s *store) DeletePKCERequestSession(context.Context, string) error { return nil }

// GetPKCERequestSession implements pkce.PKCERequestStorage.
func (s *store) GetPKCERequestSession(
	ctx context.Context, signature string, _ fosite.Session,
) (fosite.Requester, error) {
	var r Request
	if err := s.db.WithContext(ctx).First(&r, "key = ?", signature).Error; err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	client, err := s.GetClient(ctx, r.ClientID)
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	return &fosite.Request{Client: client, Form: r.pkceForm()}, nil
}

// CreateAccessTokenSession implements oauth2.CoreStorage. Access tokens are
// stateless JWTs.
func (s *store) CreateAccessTokenSession(context.Context, string, fosite.Requester) error {
	return nil
}

// GetAccessTokenSession implements oauth2.CoreStorage.
func (s *store) GetAccessTokenSession(
	_ context.Context, _ string, session fosite.Session,
) (fosite.Requester, error) {
	return &fosite.Request{Session: session}, nil
}

// DeleteAccessTokenSession implements oauth2.CoreStorage.
func (s *store) DeleteAccessTokenSession(context.Context, string) error { return nil }

// RevokeAccessToken implements oauth2.TokenRevocationStorage.
func (s *store) RevokeAccessToken(context.Context, string) error {
	return errors.New("access token revocation not supported")
}

// RevokeRefreshToken implements oauth2.TokenRevocationStorage.
func (s *store) RevokeRefreshToken(context.Context, string) error {
	return errors.New("refresh token revocation not supported")
}

// CreateRefreshTokenSession implements oauth2.CoreStorage.
func (s *store) CreateRefreshTokenSession(
	ctx context.Context, signature, _ string, request fosite.Requester,
) error {
	sess, ok := request.GetSession().(*session)
	if !ok {
		return errors.New("unexpected session type")
	}
	return s.db.WithContext(ctx).Create(&RefreshSession{
		Signature: signature,
		ClientID:  request.GetClient().GetID(),
		Subject:   sess.Subject,
		Scopes:    strings.Join(sess.Scopes, " "),
		ExpiresAt: sess.GetExpiresAt(fosite.RefreshToken),
	}).Error
}

// GetRefreshTokenSession implements oauth2.CoreStorage.
func (s *store) GetRefreshTokenSession(
	ctx context.Context, signature string, _ fosite.Session,
) (fosite.Requester, error) {
	var row RefreshSession
	if err := s.db.WithContext(ctx).First(&row, "signature = ?", signature).Error; err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	client, err := s.GetClient(ctx, row.ClientID)
	if err != nil {
		return nil, err
	}
	scopes := fosite.Arguments(strings.Fields(row.Scopes))
	return &fosite.Request{
		Client: client,
		Session: &session{
			Subject:               row.Subject,
			ClientID:              row.ClientID,
			Audience:              s.resource,
			Scopes:                scopes,
			RefreshTokenExpiresAt: row.ExpiresAt,
		},
		RequestedScope: scopes,
		GrantedScope:   scopes,
	}, nil
}

// DeleteRefreshTokenSession implements oauth2.CoreStorage.
func (s *store) DeleteRefreshTokenSession(ctx context.Context, signature string) error {
	return s.db.WithContext(ctx).Delete(&RefreshSession{}, "signature = ?", signature).Error
}

// RotateRefreshToken implements oauth2.CoreStorage.
func (s *store) RotateRefreshToken(ctx context.Context, _, refreshTokenSignature string) error {
	return s.DeleteRefreshTokenSession(ctx, refreshTokenSignature)
}
