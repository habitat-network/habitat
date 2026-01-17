package oauthserver

import (
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ory/fosite"
	"github.com/ory/fosite/handler/oauth2"
	"github.com/ory/fosite/token/jwt"
)

// authSession implements fosite.Session for authorization codes (stateless)
// We store minimal data here since the authorization code is temporary
type authSession struct {
	Subject       string
	ExpiresAt     time.Time
	ClientID      string
	Scopes        []string
	PKCEChallenge string
}

var _ fosite.Session = (*authSession)(nil)

func newAuthorizeSession(
	req fosite.AuthorizeRequester,
	did syntax.DID,
) *authSession {
	return &authSession{
		Subject:       did.String(),
		Scopes:        req.GetRequestedScopes(),
		ClientID:      req.GetClient().GetID(),
		PKCEChallenge: req.GetRequestForm().Get("code_challenge"),
	}
}

// Clone implements fosite.Session.
func (s *authSession) Clone() fosite.Session {
	return &authSession{
		Subject:       s.Subject,
		ExpiresAt:     s.ExpiresAt,
		Scopes:        append([]string{}, s.Scopes...),
		ClientID:      s.ClientID,
		PKCEChallenge: s.PKCEChallenge,
	}
}

// GetExpiresAt implements fosite.Session.
func (s *authSession) GetExpiresAt(key fosite.TokenType) time.Time {
	return s.ExpiresAt
}

// GetSubject implements fosite.Session.
func (s *authSession) GetSubject() string {
	return s.Subject
}

// GetUsername implements fosite.Session.
func (s *authSession) GetUsername() string {
	return s.Subject
}

// SetExpiresAt implements fosite.Session.
func (s *authSession) SetExpiresAt(key fosite.TokenType, exp time.Time) {
	s.ExpiresAt = exp
}

// newJWTSession creates a JWT session for access/refresh tokens from authorization session
func newJWTSession(authSess *authSession) *oauth2.JWTSession {
	return &oauth2.JWTSession{
		JWTClaims: &jwt.JWTClaims{
			Subject:   authSess.Subject,
			ExpiresAt: authSess.ExpiresAt,
			Scope:     authSess.Scopes,
		},
		JWTHeader: &jwt.Headers{},
		Subject:   authSess.Subject, // Must also set this - GetSubject() returns this field, not JWTClaims.Subject
	}
}
