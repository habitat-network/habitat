package oauthserver

import (
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/ory/fosite"
	"github.com/ory/fosite/handler/oauth2"
	"github.com/ory/fosite/handler/rfc7523"
	"github.com/ory/fosite/token/jwt"
)

// session is the single fosite.Session implementation used by every grant type
// Habitat's OAuth server supports: authorization code, refresh token, and
// RFC 7523 (JWT bearer).
//
// One value travels through the whole token lifecycle:
//   - The authorize endpoint builds it (newAuthorizeSession) and the strategy
//     encrypts it, as the stateless authorization code, via CBOR — so every
//     field is exported.
//   - At the token endpoint fosite's grant handlers mutate it in place
//     (SetSubject, SetExpiresAt) before minting tokens.
//   - The store reads it back to persist refresh tokens.
//
// Because it implements oauth2.JWTSessionContainer, DefaultJWTStrategy mints
// signed access tokens straight from it — the "sub" claim comes from Subject,
// which is why SetSubject (used by the JWT bearer handler) is all that's needed
// to get the subject into the issued token.
type session struct {
	Subject  string
	ClientID string
	Scopes   []string
	// Permissions are the requested scopes parsed into space permissions. They
	// are persisted alongside the raw Scopes so downstream code can enforce them
	// without re-parsing. Scopes that are not valid space scopes are skipped.
	Permissions           []spacePermission
	PKCEChallenge         string
	AuthCodeExpiresAt     time.Time
	AccessTokenExpiresAt  time.Time
	RefreshTokenExpiresAt time.Time
}

var (
	_ fosite.Session             = (*session)(nil)
	_ rfc7523.Session            = (*session)(nil)
	_ oauth2.JWTSessionContainer = (*session)(nil)
)

// newAuthorizeSession builds the session embedded in a freshly issued
// authorization code, capturing who authorized (did) and the request's scopes
// and PKCE challenge.
func newAuthorizeSession(req fosite.AuthorizeRequester, did syntax.DID) *session {
	scopes := req.GetRequestedScopes()
	return &session{
		Subject:       did.String(),
		ClientID:      req.GetClient().GetID(),
		Scopes:        scopes,
		Permissions:   parseSpacePermissions(scopes),
		PKCEChallenge: req.GetRequestForm().Get("code_challenge"),
	}
}

// newSession returns the empty session handed to the token endpoint. Grant
// handlers either populate it in place (the JWT bearer handler calls
// SetSubject/SetExpiresAt) or replace it outright with one loaded from storage.
func newSession() *session {
	return &session{}
}

// getAuthCode serializes the session into the stateless authorization code: an
// encrypted CBOR blob that is itself the code (and its signature). Only the
// fields set at authorize time — subject, client, scopes, PKCE challenge — carry
// meaning; the token-expiry fields are still zero and are filled in by fosite
// once the code is redeemed.
func (s *session) getAuthCode(encryptionKey []byte) (string, error) {
	return encrypt.EncryptCBOR(s, encryptionKey)
}

// decodeSession decrypts an authorization code produced by getAuthCode back into
// the session it was minted from.
func decodeSession(authCode string, encryptionKey []byte) (*session, error) {
	var s session
	if err := encrypt.DecryptCBOR(authCode, encryptionKey, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// GetJWTClaims implements oauth2.JWTSessionContainer. DefaultJWTStrategy calls
// this to seed the access token's claims, then layers on the granted scopes,
// audience, and expiry. We only need to surface the subject here so that a
// subject set via SetSubject lands in the token's "sub" claim.
func (s *session) GetJWTClaims() jwt.JWTClaimsContainer {
	return &jwt.JWTClaims{
		Subject:   s.Subject,
		ExpiresAt: s.AccessTokenExpiresAt,
		Audience:  []string{s.ClientID},
	}
}

// GetJWTHeader implements oauth2.JWTSessionContainer.
func (s *session) GetJWTHeader() *jwt.Headers {
	return &jwt.Headers{}
}

// Clone implements [fosite.Session].
func (s *session) Clone() fosite.Session {
	clone := *s
	clone.Scopes = append([]string{}, s.Scopes...)
	clone.Permissions = append([]spacePermission{}, s.Permissions...)
	return &clone
}

// GetExpiresAt implements [fosite.Session].
func (s *session) GetExpiresAt(key fosite.TokenType) time.Time {
	switch key {
	case fosite.AccessToken:
		return s.AccessTokenExpiresAt
	case fosite.RefreshToken:
		return s.RefreshTokenExpiresAt
	case fosite.AuthorizeCode:
		return s.AuthCodeExpiresAt
	}
	return time.Time{}
}

// SetExpiresAt implements [fosite.Session].
func (s *session) SetExpiresAt(key fosite.TokenType, exp time.Time) {
	switch key {
	case fosite.AccessToken:
		s.AccessTokenExpiresAt = exp
	case fosite.RefreshToken:
		s.RefreshTokenExpiresAt = exp
	case fosite.AuthorizeCode:
		s.AuthCodeExpiresAt = exp
	}
}

// GetSubject implements [fosite.Session].
func (s *session) GetSubject() string {
	return s.Subject
}

// SetSubject implements [rfc7523.Session].
func (s *session) SetSubject(subject string) {
	s.Subject = subject
}

// GetUsername implements [fosite.Session].
func (s *session) GetUsername() string {
	return ""
}
