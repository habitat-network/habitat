package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/gorilla/sessions"
	"github.com/rs/zerolog/log"
)

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

type Session interface {
	GetDpopKey() (*ecdsa.PrivateKey, bool, error)
	SetDpopKey(*ecdsa.PrivateKey) error

	GetDpopNonce() (string, bool, error)
	SetDpopNonce(string) error

	GetTokenInfo() (*TokenResponse, bool, error)
	SetTokenInfo(*TokenResponse) error

	GetIdentity() (*identity.Identity, bool, error)
	SetIdentity(*identity.Identity) error

	GetPDSURL() (string, bool, error)
	SetPDSURL(string) error

	GetIssuer() (string, bool, error)
	SetIssuer(string) error
}

const (
	cKeySessionKey       = "key"
	cNonceSessionKey     = "nonce"
	cTokenInfoSessionKey = "token_info"
	cIssuerSessionKey    = "issuer"
	cIdentitySessionKey  = "identity"
	cPDSURLSessionKey    = "pds_url"
)

type ErrorSessionValueNotFound struct {
	Key string
}

func (e *ErrorSessionValueNotFound) Error() string {
	return fmt.Sprintf("session value not found: %s", e.Key)
}

type cookieSession struct {
	// Internally, use a Gorilla session to store the session data
	session *sessions.Session
}

func newCookieSession(
	r *http.Request,
	sessionStore sessions.Store,
	id *identity.Identity,
	pdsURL string,
) (*cookieSession, error) {
	session, err := sessionStore.New(r, "dpop-session")
	if err != nil {
		return nil, err
	}

	// TODO inject the key
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	dpopSession := &cookieSession{session: session}

	// Use session class methods instead of manually modifying session values
	err = dpopSession.SetDpopKey(key)
	if err != nil {
		return nil, err
	}

	err = dpopSession.SetIdentity(id)
	if err != nil {
		return nil, err
	}

	err = dpopSession.SetPDSURL(pdsURL)
	if err != nil {
		return nil, err
	}

	return dpopSession, nil
}

func getCookieSession(r *http.Request, sessionStore sessions.Store) (*cookieSession, error) {
	session, err := sessionStore.Get(r, "dpop-session")
	if err != nil {
		return nil, err
	}

	// Check that the session has a valid key, so we can fail fast if the session is somehow invalid
	_, ok := session.Values[cKeySessionKey]
	if !ok {
		return nil, errors.New("invalid/missing key in session")
	}

	return &cookieSession{session: session}, nil
}

// Save writes the session out to the response writer. It is designed to be called with defer.
func (s *cookieSession) Save(r *http.Request, w http.ResponseWriter) {
	err := s.session.Save(r, w)
	if err != nil {
		log.Error().Err(err).Msg("error saving session")
		w.WriteHeader(http.StatusInternalServerError)
		_, err := w.Write([]byte(err.Error()))
		if err != nil {
			log.Error().Err(err).Msg("error writing error to response")
		}
	}
}

func (s *cookieSession) SetDpopKey(key *ecdsa.PrivateKey) error {
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	s.session.Values[cKeySessionKey] = keyBytes
	return nil
}

func (s *cookieSession) GetDpopKey() (*ecdsa.PrivateKey, bool, error) {
	keyBytes, ok := s.session.Values[cKeySessionKey]
	if !ok {
		return nil, false, nil
	}
	bytes, okCast := keyBytes.([]byte)
	if !okCast {
		return nil, false, errors.New("key in session is not a []byte")
	}
	key, err := x509.ParseECPrivateKey(bytes)
	if err != nil {
		return nil, false, err
	}
	return key, true, nil
}

func (s *cookieSession) SetTokenInfo(tokenResp *TokenResponse) error {
	marshalled, err := json.Marshal(tokenResp)
	if err != nil {
		return err
	}

	s.session.Values[cTokenInfoSessionKey] = marshalled

	return nil
}

func (s *cookieSession) GetTokenInfo() (*TokenResponse, bool, error) {
	marshalled, ok := s.session.Values[cTokenInfoSessionKey]
	if !ok {
		return nil, false, nil
	}
	bytes, okCast := marshalled.([]byte)
	if !okCast {
		return nil, false, errors.New("token info in session is not a []byte")
	}
	var tokenResp TokenResponse
	err := json.Unmarshal(bytes, &tokenResp)
	if err != nil {
		return nil, false, err
	}
	return &tokenResp, true, nil
}

func (s *cookieSession) SetIssuer(issuer string) error {
	s.session.Values[cIssuerSessionKey] = issuer
	return nil
}

func (s *cookieSession) GetIssuer() (string, bool, error) {
	v, ok := s.session.Values[cIssuerSessionKey]
	if !ok {
		return "", false, nil
	}
	issuer, okCast := v.(string)
	if !okCast {
		return "", false, errors.New("issuer in session is not a string")
	}
	return issuer, true, nil
}

func (s *cookieSession) SetPDSURL(pdsURL string) error {
	s.session.Values[cPDSURLSessionKey] = pdsURL
	return nil
}

func (s *cookieSession) GetPDSURL() (string, bool, error) {
	v, ok := s.session.Values[cPDSURLSessionKey]
	if !ok {
		return "", false, nil
	}
	url, okCast := v.(string)
	if !okCast {
		return "", false, errors.New("PDS URL in session is not a string")
	}
	return url, true, nil
}

func (s *cookieSession) GetIdentity() (*identity.Identity, bool, error) {
	v, ok := s.session.Values[cIdentitySessionKey]
	if !ok {
		return nil, false, nil
	}
	bytes, okCast := v.([]byte)
	if !okCast {
		return nil, false, errors.New("identity in session is not a []byte")
	}
	var i identity.Identity
	err := json.Unmarshal(bytes, &i)
	if err != nil {
		return nil, false, err
	}
	return &i, true, nil
}

func (s *cookieSession) SetIdentity(id *identity.Identity) error {
	identityBytes, err := json.Marshal(id)
	if err != nil {
		return err
	}
	s.session.Values[cIdentitySessionKey] = identityBytes
	return nil
}

func (s *cookieSession) SetDpopNonce(nonce string) error {
	s.session.Values[cNonceSessionKey] = nonce
	return nil
}

func (s *cookieSession) GetDpopNonce() (string, bool, error) {
	v, ok := s.session.Values[cNonceSessionKey]
	if !ok {
		return "", false, nil
	}
	nonce, okCast := v.(string)
	if !okCast {
		return "", false, errors.New("nonce in session is not a string")
	}
	return nonce, true, nil
}
