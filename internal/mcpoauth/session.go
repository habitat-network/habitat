package mcpoauth

import (
	"time"

	"github.com/ory/fosite"
	"github.com/ory/fosite/handler/oauth2"
	"github.com/ory/fosite/token/jwt"
)

// session is the fosite.Session for every grant this server issues. It
// implements oauth2.JWTSessionContainer so access tokens are minted as signed
// JWTs straight from it: "sub" is the authenticated account's DID and "aud" is
// the MCP resource the token is good for.
type session struct {
	Subject               string
	ClientID              string
	Scopes                []string
	AuthCodeExpiresAt     time.Time
	AccessTokenExpiresAt  time.Time
	RefreshTokenExpiresAt time.Time
}

var (
	_ fosite.Session             = (*session)(nil)
	_ oauth2.JWTSessionContainer = (*session)(nil)
)

// GetJWTClaims implements oauth2.JWTSessionContainer.
func (s *session) GetJWTClaims() jwt.JWTClaimsContainer {
	return &jwt.JWTClaims{
		Subject:   s.Subject,
		ExpiresAt: s.AccessTokenExpiresAt,
		Audience:  []string{s.ClientID},
	}
}

// GetJWTHeader implements oauth2.JWTSessionContainer. The "typ" here is
// internal/oauthserver's own marker for a token it should handle (see
// OAuthServer.CanHandle) — this server shares that server's exact signing key
// (see New) so the two are, deliberately, the same token type: a token from
// either server works against either server's resources.
func (s *session) GetJWTHeader() *jwt.Headers {
	return &jwt.Headers{Extra: map[string]any{"typ": "oauth+JWT"}}
}

// Clone implements fosite.Session.
func (s *session) Clone() fosite.Session {
	clone := *s
	clone.Scopes = append([]string{}, s.Scopes...)
	return &clone
}

// GetExpiresAt implements fosite.Session.
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

// SetExpiresAt implements fosite.Session.
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

// GetSubject implements fosite.Session.
func (s *session) GetSubject() string { return s.Subject }

// GetUsername implements fosite.Session.
func (s *session) GetUsername() string { return "" }
