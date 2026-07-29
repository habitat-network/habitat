package oauthserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	jose "github.com/go-jose/go-jose/v3"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/ory/fosite"
	"github.com/ory/fosite/handler/oauth2"
	"github.com/ory/fosite/handler/pkce"
	"github.com/ory/fosite/handler/rfc7523"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("github.com/habitat-network/habitat/internal/oauthserver")

type store struct {
	db                       *gorm.DB
	approvedJwtBearerClients ApprovedClientStore
}

// OAuthRequest is the single row type backing every short-lived piece of OAuth
// flow state: pushed authorization requests, in-flight authorization requests
// bridging the PDS redirect, and issued authorization codes. Which one a row is
// depends only on what its Key is (a PAR request_uri, a cookie-held request key,
// or an authorization code signature).
type OAuthRequest struct {
	Key      string `gorm:"primaryKey"`
	ClientID string `gorm:"size:1024"`
	// Subject is the resolved DID this flow authenticates. Empty until the login
	// hint is resolved (at PAR time) or the handle is resolved (at authorize time).
	Subject             string    `gorm:"size:255"`
	Scopes              string    `gorm:"size:512"` // space-separated
	CodeChallenge       string    `gorm:"size:255"`
	CodeChallengeMethod string    `gorm:"size:32"`
	RedirectURI         string    `gorm:"size:1024"`
	State               string    `gorm:"size:1024"` // the client's own OAuth state param
	ResponseType        string    `gorm:"size:64"`
	ExpiresAt           time.Time `gorm:"index"`
}

// toAuthorizeRequest rebuilds the fosite.AuthorizeRequest this row was stored
// from. The redirect URI is parsed rather than left in the form because
// WriteAuthorizeResponse reads the struct field, not the form.
func (r *OAuthRequest) toAuthorizeRequest(client fosite.Client) (*fosite.AuthorizeRequest, error) {
	scopes := fosite.Arguments{}
	if r.Scopes != "" {
		scopes = strings.Split(r.Scopes, " ")
	}
	form := url.Values{}
	setIfNotEmpty := func(k, v string) {
		if v != "" {
			form.Set(k, v)
		}
	}
	setIfNotEmpty("client_id", r.ClientID)
	setIfNotEmpty("redirect_uri", r.RedirectURI)
	setIfNotEmpty("response_type", r.ResponseType)
	setIfNotEmpty("scope", r.Scopes)
	setIfNotEmpty("state", r.State)
	setIfNotEmpty("code_challenge", r.CodeChallenge)
	setIfNotEmpty("code_challenge_method", r.CodeChallengeMethod)

	var redirectURI *url.URL
	if r.RedirectURI != "" {
		var err error
		redirectURI, err = url.Parse(r.RedirectURI)
		if err != nil {
			return nil, fmt.Errorf("failed to parse stored redirect uri: %w", err)
		}
	}

	return &fosite.AuthorizeRequest{
		ResponseTypes:        fosite.Arguments(strings.Fields(r.ResponseType)),
		HandledResponseTypes: fosite.Arguments{},
		RedirectURI:          redirectURI,
		State:                r.State,
		Request: fosite.Request{
			Client:         client,
			Session:        &session{Subject: r.Subject, ClientID: r.ClientID, Scopes: scopes},
			RequestedScope: scopes,
			GrantedScope:   scopes,
			Form:           form,
			RequestedAt:    time.Now().UTC(),
		},
	}, nil
}

// fromRequester populates the OAuthRequest columns from a fosite.Requester.
func (r *OAuthRequest) fromRequester(key string, requester fosite.Requester, expiresAt time.Time) {
	form := requester.GetRequestForm()
	r.Key = key
	r.ClientID = requester.GetClient().GetID()
	r.Scopes = strings.Join(requester.GetRequestedScopes(), " ")
	r.RedirectURI = form.Get("redirect_uri")
	r.State = form.Get("state")
	r.ResponseType = form.Get("response_type")
	r.CodeChallenge = form.Get("code_challenge")
	r.CodeChallengeMethod = form.Get("code_challenge_method")
	r.ExpiresAt = expiresAt
	if sess := requester.GetSession(); sess != nil {
		r.Subject = sess.GetSubject()
	}
}

// pkceForm reconstructs the code_challenge/code_challenge_method form values
// for the PKCE handler from a stored request row.
func (r *OAuthRequest) pkceForm() url.Values {
	v := url.Values{}
	if r.CodeChallenge != "" {
		v.Set("code_challenge", r.CodeChallenge)
	}
	if r.CodeChallengeMethod != "" {
		v.Set("code_challenge_method", r.CodeChallengeMethod)
	}
	return v
}

type OAuthSession struct {
	Signature string `gorm:"primaryKey"`
	ClientID  string
	Subject   string // DID of the user
	Scopes    string // Space-separated scopes
	ExpiresAt time.Time
}

type ConnectedApp struct {
	Subject  string `gorm:"primaryKey,uniqueIndex:idx_connected_app"` // user DID
	ClientID string `gorm:"primaryKey,uniqueIndex:idx_connected_app"` // client_id URL
	Scopes   string // Space-separated scopes
	// GORM auto-managed
	CreatedAt time.Time
	UpdatedAt time.Time
}

func newStore(
	db *gorm.DB,
	approvedJwtBearerClients ApprovedClientStore,
) (*store, error) {
	err := db.AutoMigrate(&OAuthRequest{}, &OAuthSession{}, &ConnectedApp{})
	if err != nil {
		return nil, err
	}
	return &store{
		db:                       db,
		approvedJwtBearerClients: approvedJwtBearerClients,
	}, nil
}

var (
	_ fosite.Storage                = (*store)(nil)
	_ fosite.PARStorage             = (*store)(nil)
	_ oauth2.CoreStorage            = (*store)(nil)
	_ oauth2.TokenRevocationStorage = (*store)(nil)
	_ pkce.PKCERequestStorage       = (*store)(nil)
	_ rfc7523.RFC7523KeyStorage     = (*store)(nil)
)

// parSessionTTL bounds how long an in-flight authorization request — pushed or
// mid-PDS-redirect — stays resumable.
const parSessionTTL = 10 * time.Minute

// CreatePARSession implements fosite.PARStorage. It also doubles as the
// re-store step the authorize flow uses to persist an in-flight request under
// a fresh key while it bridges the PDS redirect; Save (upsert) rather than
// Create so re-storing the same key (e.g. once the login hint resolves to a
// DID) does not hit a duplicate-key error.
func (s *store) CreatePARSession(
	ctx context.Context,
	requestURI string,
	request fosite.AuthorizeRequester,
) error {
	var r OAuthRequest
	r.fromRequester(requestURI, request, time.Now().Add(parSessionTTL))
	return s.db.WithContext(ctx).Save(&r).Error
}

// GetPARSession implements fosite.PARStorage.
func (s *store) GetPARSession(
	ctx context.Context,
	requestURI string,
) (fosite.AuthorizeRequester, error) {
	var r OAuthRequest
	err := s.db.WithContext(ctx).First(&r, "key = ?", requestURI).Error
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	if time.Now().After(r.ExpiresAt) {
		return nil, fosite.ErrNotFound
	}
	client, err := s.GetClient(ctx, r.ClientID)
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	return r.toAuthorizeRequest(client)
}

// DeletePARSession implements fosite.PARStorage.
func (s *store) DeletePARSession(ctx context.Context, requestURI string) error {
	return s.db.WithContext(ctx).Delete(&OAuthRequest{}, "key = ?", requestURI).Error
}

// ClientAssertionJWTValid implements fosite.Storage. Client assertion JWTs are
// never checked (see SetClientAssertionJWT), so there is nothing to validate.
func (s *store) ClientAssertionJWTValid(ctx context.Context, jti string) error {
	return nil
}

// GetClient implements fosite.Storage.
func (s *store) GetClient(ctx context.Context, id string) (fosite.Client, error) {
	ctx, span := tracer.Start(ctx, "GetClient")
	defer span.End()
	span.SetAttributes(attribute.String("client_id", id))
	metadata, err := s.fetchClientMetadata(ctx, id)
	if err != nil {
		return nil, err
	}
	return &client{metadata}, nil
}

// fetchClientMetadata fetches and decodes the client metadata document
// published at id (the client's client_id URL). See
// https://atproto.com/specs/oauth#client-id-metadata-document.
func (s *store) fetchClientMetadata(
	ctx context.Context,
	id string,
) (*pdsclient.ClientMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, id, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	// TODO: consider caching
	cl := http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch client metadata: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch client metadata: status %d", resp.StatusCode)
	}

	var metadata pdsclient.ClientMetadata
	err = json.NewDecoder(resp.Body).Decode(&metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to decode client metadata: %w", err)
	}
	return &metadata, nil
}

// GetPublicKey implements rfc7523.RFC7523KeyStorage. issuer is the "iss"
// claim of the JWT bearer assertion, expected to be a client ID (client
// metadata URL) present in the hardcoded jwtBearerAllowedClients allow-list.
func (s *store) GetPublicKey(
	ctx context.Context,
	issuer string,
	subject string,
	keyID string,
) (*jose.JSONWebKey, error) {
	keys, err := s.GetPublicKeys(ctx, issuer, subject)
	if err != nil {
		return nil, err
	}
	for _, key := range keys.Keys {
		if key.KeyID == keyID {
			return &key, nil
		}
	}
	return nil, fosite.ErrNotFound
}

// GetPublicKeys implements rfc7523.RFC7523KeyStorage.
func (s *store) GetPublicKeys(
	ctx context.Context,
	issuer string,
	_ string,
) (*jose.JSONWebKeySet, error) {
	if !s.approvedJwtBearerClients.IsApprovedClient(issuer) {
		return nil, fosite.ErrNotFound
	}
	metadata, err := s.fetchClientMetadata(ctx, issuer)
	if err != nil {
		return nil, err
	}
	if metadata.Jwks == nil || len(metadata.Jwks.Keys) == 0 {
		return nil, fosite.ErrNotFound
	}
	return metadata.Jwks, nil
}

// GetPublicKeyScopes implements rfc7523.RFC7523KeyStorage.
func (s *store) GetPublicKeyScopes(
	ctx context.Context,
	issuer string,
	_ string,
	_ string,
) ([]string, error) {
	if !s.approvedJwtBearerClients.IsApprovedClient(issuer) {
		return nil, fosite.ErrNotFound
	}
	cl, err := s.GetClient(ctx, issuer)
	if err != nil {
		return nil, err
	}
	return cl.GetScopes(), nil
}

// IsJWTUsed implements rfc7523.RFC7523KeyStorage. Assertion JTIs are never
// tracked (see SetClientAssertionJWT), so none are ever considered used.
func (s *store) IsJWTUsed(ctx context.Context, jti string) (bool, error) {
	return false, nil
}

// MarkJWTUsedForTime implements rfc7523.RFC7523KeyStorage.
func (s *store) MarkJWTUsedForTime(ctx context.Context, jti string, exp time.Time) error {
	return nil
}

// SetClientAssertionJWT implements fosite.Storage. Client assertion JWTs are
// never checked by this server (see ClientAssertionJWTValid), so tracking
// them is unnecessary.
func (s *store) SetClientAssertionJWT(ctx context.Context, jti string, exp time.Time) error {
	return nil
}

// CreateAuthorizeCodeSession implements oauth2.CoreStorage.
func (s *store) CreateAuthorizeCodeSession(
	ctx context.Context,
	signature string,
	requester fosite.Requester,
) (err error) {
	var r OAuthRequest
	// Authorize code sessions expire based on fosite config (typically 10-15 min).
	exp := time.Now().Add(15 * time.Minute)
	if sess := requester.GetSession(); sess != nil {
		if t := sess.GetExpiresAt(fosite.AuthorizeCode); !t.IsZero() {
			exp = t
		}
	}
	r.fromRequester(signature, requester, exp)
	return s.db.WithContext(ctx).Create(&r).Error
}

// GetAuthorizeCodeSession implements oauth2.CoreStorage.
func (s *store) GetAuthorizeCodeSession(
	ctx context.Context,
	signature string,
	_ fosite.Session,
) (request fosite.Requester, err error) {
	ctx, span := tracer.Start(ctx, "GetAuthorizeCodeSession")
	defer span.End()
	span.SetAttributes(attribute.String("auth_signature", signature))

	var r OAuthRequest
	err = s.db.WithContext(ctx).First(&r, "key = ?", signature).Error
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	client, err := s.GetClient(ctx, r.ClientID)
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	ar, err := r.toAuthorizeRequest(client)
	if err != nil {
		return nil, err
	}
	// The stateless code carries the scopes granted at authorize time. Mark
	// them granted (not just requested) so fosite echoes them in the token
	// response; atproto clients reject a token response with an empty scope.
	ar.Request.Session = &session{
		Subject:           r.Subject,
		ClientID:          r.ClientID,
		Scopes:            strings.Fields(r.Scopes),
		PKCEChallenge:     r.CodeChallenge,
		AuthCodeExpiresAt: r.ExpiresAt,
	}
	return &ar.Request, nil
}

// InvalidateAuthorizeCodeSession implements oauth2.CoreStorage.
func (s *store) InvalidateAuthorizeCodeSession(ctx context.Context, signature string) (err error) {
	return s.db.WithContext(ctx).Delete(&OAuthRequest{}, "key = ?", signature).Error
}

// CreatePKCERequestSession implements pkce.PKCERequestStorage. PKCE data is
// stored as part of the authorize code session row, so there is nothing extra
// to persist here.
func (s *store) CreatePKCERequestSession(
	ctx context.Context,
	signature string,
	requester fosite.Requester,
) error {
	return nil
}

// DeletePKCERequestSession implements pkce.PKCERequestStorage. Deleting the
// authorize code session (InvalidateAuthorizeCodeSession) covers this row.
func (s *store) DeletePKCERequestSession(ctx context.Context, signature string) error {
	return nil
}

// GetPKCERequestSession implements pkce.PKCERequestStorage.
func (s *store) GetPKCERequestSession(
	ctx context.Context,
	signature string,
	_ fosite.Session,
) (fosite.Requester, error) {
	var r OAuthRequest
	err := s.db.WithContext(ctx).First(&r, "key = ?", signature).Error
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}
	return &fosite.Request{
		Form: r.pkceForm(),
	}, nil
}

// CreateAccessTokenSession implements oauth2.CoreStorage.
func (s *store) CreateAccessTokenSession(
	_ context.Context,
	_ string,
	_ fosite.Requester,
) (err error) {
	return nil
}

// GetAccessTokenSession implements oauth2.CoreStorage.
func (s *store) GetAccessTokenSession(
	ctx context.Context,
	signature string,
	session fosite.Session,
) (fosite.Requester, error) {
	return &fosite.Request{Session: session}, nil
}

// DeleteAccessTokenSession implements oauth2.CoreStorage.
func (s *store) DeleteAccessTokenSession(_ context.Context, _ string) error {
	return nil
}

// RevokeAccessToken implements oauth2.TokenRevocationStorage.
func (s *store) RevokeAccessToken(ctx context.Context, requestID string) error {
	return fmt.Errorf("access token revocation not supported")
}

// CreateRefreshTokenSession implements oauth2.CoreStorage.
func (s *store) CreateRefreshTokenSession(
	ctx context.Context,
	signature string,
	accessSignature string,
	request fosite.Requester,
) error {
	ctx, span := tracer.Start(ctx, "CreateRefreshTokenSession")
	defer span.End()
	span.SetAttributes(
		attribute.String("refresh_signature", signature),
		attribute.String("access_signature", accessSignature),
		attribute.String("client_id", request.GetClient().GetID()),
	)

	sess := request.GetSession().(*session)
	oauthSession := &OAuthSession{
		Signature: signature,
		ClientID:  request.GetClient().GetID(),
		Subject:   sess.Subject,
		Scopes:    strings.Join(sess.Scopes, " "),
		ExpiresAt: sess.GetExpiresAt(fosite.RefreshToken),
	}

	err := s.db.WithContext(ctx).Create(oauthSession).Error
	if err != nil {
		return err
	}

	return s.db.WithContext(ctx).
		Where(ConnectedApp{Subject: oauthSession.Subject, ClientID: oauthSession.ClientID}).
		Assign(ConnectedApp{Scopes: oauthSession.Scopes}).
		FirstOrCreate(&ConnectedApp{}).Error
}

// DeleteRefreshTokenSession implements oauth2.CoreStorage.
func (s *store) DeleteRefreshTokenSession(ctx context.Context, signature string) error {
	ctx, span := tracer.Start(ctx, "DeleteRefreshTokenSession")
	defer span.End()
	span.SetAttributes(attribute.String("refresh_signature", signature))
	return s.db.WithContext(ctx).Delete(&OAuthSession{}, "signature = ?", signature).Error
}

// GetRefreshTokenSession implements oauth2.CoreStorage.
func (s *store) GetRefreshTokenSession(
	ctx context.Context,
	signature string,
	_ fosite.Session,
) (fosite.Requester, error) {
	ctx, span := tracer.Start(ctx, "GetRefreshTokenSession")
	defer span.End()
	span.SetAttributes(attribute.String("refresh_signature", signature))

	var oauthSession OAuthSession

	err := s.db.WithContext(ctx).First(&oauthSession, "signature = ?", signature).Error
	if err != nil {
		return nil, errors.Join(fosite.ErrNotFound, err)
	}

	client, err := s.GetClient(ctx, oauthSession.ClientID)
	if err != nil {
		return nil, err
	}

	scopes := fosite.Arguments{}
	if oauthSession.Scopes != "" {
		scopes = strings.Split(oauthSession.Scopes, " ")
	}

	return &fosite.Request{
		Client: client,
		Session: &session{
			Subject:               oauthSession.Subject,
			ClientID:              oauthSession.ClientID,
			Scopes:                scopes,
			RefreshTokenExpiresAt: oauthSession.ExpiresAt,
		},
		RequestedScope: scopes,
		GrantedScope:   scopes,
	}, nil
}

// RotateRefreshToken implements oauth2.CoreStorage.
func (s *store) RotateRefreshToken(
	ctx context.Context,
	requestID string,
	refreshTokenSignature string,
) (err error) {
	ctx, span := tracer.Start(ctx, "RotateRefreshToken")
	defer span.End()
	span.SetAttributes(
		attribute.String("request_id", requestID),
		attribute.String("refresh_signature", refreshTokenSignature),
	)
	return s.DeleteRefreshTokenSession(ctx, refreshTokenSignature)
}

// RevokeRefreshToken implements oauth2.TokenRevocationStorage.
func (s *store) RevokeRefreshToken(_ context.Context, _ string) error {
	return fmt.Errorf("refresh token revocation not supported")
}
