package oauthserver

import (
	"time"

	"github.com/ory/fosite"
	"github.com/ory/fosite/handler/oauth2"
	"github.com/ory/fosite/handler/rfc7523"
	"github.com/ory/fosite/token/jwt"
)

// session is the single fosite.Session implementation used by every grant type
// Habitat's OAuth server supports: authorization code, refresh token, and
// RFC 7523 (JWT bearer).
//
// Because it implements oauth2.JWTSessionContainer, DefaultJWTStrategy mints
// signed access tokens straight from it — the "sub" claim comes from Subject,
// which is why SetSubject (used by the JWT bearer handler) is all that's needed
// to get the subject into the issued token.
type session struct {
	Subject string
	// Authorizer is the member who approved a session acting as a group. It
	// is empty for every other session. A group's access is narrowed by the
	// authorizer's current roles on every request, so it ends with their
	// membership.
	Authorizer            string
	ClientID              string
	Scopes                []string
	AuthCodeExpiresAt     time.Time
	AccessTokenExpiresAt  time.Time
	RefreshTokenExpiresAt time.Time
}

// AuthorizerClaim is the access token claim naming the member who authorized a
// session that acts as a group.
const AuthorizerClaim = "authorizer"

var (
	_ fosite.Session             = (*session)(nil)
	_ rfc7523.Session            = (*session)(nil)
	_ oauth2.JWTSessionContainer = (*session)(nil)
)

// newSession returns the empty session handed to the token endpoint. Grant
// handlers either populate it in place (the JWT bearer handler calls
// SetSubject/SetExpiresAt) or replace it outright with one loaded from storage.
func newSession() *session {
	return &session{}
}

// GetJWTClaims implements oauth2.JWTSessionContainer. DefaultJWTStrategy calls
// this to seed the access token's claims, then layers on the granted scopes,
// audience, and expiry. We only need to surface the subject here so that a
// subject set via SetSubject lands in the token's "sub" claim.
func (s *session) GetJWTClaims() jwt.JWTClaimsContainer {
	claims := &jwt.JWTClaims{
		Subject:   s.Subject,
		ExpiresAt: s.AccessTokenExpiresAt,
		Audience:  []string{s.ClientID},
	}
	if s.Authorizer != "" {
		claims.Extra = map[string]any{AuthorizerClaim: s.Authorizer}
	}
	return claims
}

// GetJWTHeader implements oauth2.JWTSessionContainer.
func (s *session) GetJWTHeader() *jwt.Headers {
	return &jwt.Headers{
		Extra: map[string]any{
			"typ": "oauth+JWT",
		},
	}
}

// Clone implements [fosite.Session].
func (s *session) Clone() fosite.Session {
	clone := *s
	clone.Scopes = append([]string{}, s.Scopes...)
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

// GetAuthorizer returns the member who approved this session, if any.
func (s *session) GetAuthorizer() string {
	return s.Authorizer
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
