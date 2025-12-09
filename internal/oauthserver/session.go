package oauthserver

import (
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/eagraf/habitat-new/internal/oauthclient"
	"github.com/ory/fosite"
)

type authSession struct {
	Subject       string                     `cbor:"1,keyasint"`
	ExpiresAt     time.Time                  `cbor:"2,keyasint"`
	DpopKey       []byte                     `cbor:"3,keyasint"`
	TokenInfo     *oauthclient.TokenResponse `cbor:"4,keyasint"`
	ClientID      string                     `cbor:"5,keyasint"`
	Scopes        []string                   `cbor:"7,keyasint"`
	State         string                     `cbor:"8,keyasint"`
	PKCEChallenge string                     `cbor:"9,keyasint"`
}

var _ fosite.Session = (*authSession)(nil)

func newAuthorizeSession(
	req fosite.AuthorizeRequester,
	dpopKey []byte,
	tokenInfo *oauthclient.TokenResponse,
	did syntax.DID,
) *authSession {
	return &authSession{
		Subject:       did.String(),
		Scopes:        req.GetRequestedScopes(),
		DpopKey:       dpopKey,
		TokenInfo:     tokenInfo,
		State:         req.GetState(),
		ClientID:      req.GetClient().GetID(),
		PKCEChallenge: req.GetRequestForm().Get("code_challenge"),
	}
}

func newAccessTokenSession(session *authSession) *authSession {
	return &authSession{
		Subject:   session.Subject,
		DpopKey:   session.DpopKey,
		TokenInfo: session.TokenInfo,
		Scopes:    session.Scopes,
	}
}

// Clone implements fosite.Session.
func (s *authSession) Clone() fosite.Session {
	return s
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
