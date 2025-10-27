package oauthserver

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/gob"
	"time"

	"github.com/eagraf/habitat-new/internal/auth"
	"github.com/ory/fosite"
)

type session struct {
	ExpiresAt    time.Time
	DpopKey      *ecdsa.PrivateKey
	AccessToken  string
	RefreshToken string
	Subject      string
}

var _ fosite.Session = (*session)(nil)

func newSession(subject string, dpopKey *ecdsa.PrivateKey) *session {
	return &session{
		Subject: subject,
		DpopKey: dpopKey,
	}
}

// Clone implements fosite.Session.
func (s *session) Clone() fosite.Session {
	return s
}

// GetExpiresAt implements fosite.Session.
func (s *session) GetExpiresAt(key fosite.TokenType) time.Time {
	return s.ExpiresAt
}

// GetSubject implements fosite.Session.
func (s *session) GetSubject() string {
	return s.Subject
}

// GetUsername implements fosite.Session.
func (s *session) GetUsername() string {
	return s.Subject
}

// SetExpiresAt implements fosite.Session.
func (s *session) SetExpiresAt(key fosite.TokenType, exp time.Time) {
	s.ExpiresAt = exp
}

func (s *session) SetTokenInfo(tokenInfo *auth.TokenResponse) {
	s.AccessToken = tokenInfo.AccessToken
	s.RefreshToken = tokenInfo.RefreshToken
	// s.ExpiresAt = time.Now().UTC().Add(time.Duration(tokenInfo.ExpiresIn))
}

func (s *session) GobEncode() ([]byte, error) {
	buf := new(bytes.Buffer)
	dpopKeyBytes, err := s.DpopKey.Bytes()
	if err != nil {
		return nil, err
	}
	expiresAtBytes, err := s.ExpiresAt.MarshalBinary()
	if err != nil {
		return nil, err
	}
	err = gob.NewEncoder(buf).Encode(map[string]any{
		"ExpiresAt":    expiresAtBytes,
		"AccessToken":  s.AccessToken,
		"RefreshToken": s.RefreshToken,
		"Subject":      s.Subject,
		"DpopKey":      dpopKeyBytes,
	})
	return buf.Bytes(), err
}

func (s *session) GobDecode(data []byte) error {
	var decoded map[string]any
	err := gob.NewDecoder(bytes.NewBuffer(data)).Decode(&decoded)
	if err != nil {
		return err
	}
	if expiresAtBytes, ok := decoded["ExpiresAt"].([]byte); ok && len(expiresAtBytes) > 0 {
		if err := s.ExpiresAt.UnmarshalBinary(expiresAtBytes); err != nil {
			return err
		}
	}
	if accessToken, ok := decoded["AccessToken"].(string); ok {
		s.AccessToken = accessToken
	}
	if refreshToken, ok := decoded["RefreshToken"].(string); ok {
		s.RefreshToken = refreshToken
	}
	if subject, ok := decoded["Subject"].(string); ok {
		s.Subject = subject
	}
	if dpopKeyBytes, ok := decoded["DpopKey"].([]byte); ok {
		s.DpopKey, err = ecdsa.ParseRawPrivateKey(elliptic.P256(), dpopKeyBytes)
		if err != nil {
			return err
		}
	}
	return nil
}
