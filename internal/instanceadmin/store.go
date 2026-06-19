package instanceadmin

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/gorilla/sessions"
)

const (
	sessionName          = "habitat_admin_session"
	sessionDuration      = 24 * time.Hour
	sessionAuthenticated = "authenticated"
)

// Store manages the instance admin credential and its sessions. Neither is
// persisted: the credential lives only in process memory (see Bootstrap), and
// sessions are encoded directly into a signed/encrypted cookie via
// gorilla/sessions, so both reset on restart.
type Store interface {
	// Bootstrap sets the in-memory instance admin credential for this process:
	// presetPassword if non-empty, otherwise a freshly generated random password.
	// The plaintext password is returned only when generated is true, so callers can
	// surface it to the operator exactly once.
	Bootstrap(
		ctx context.Context,
		presetPassword string,
	) (password string, generated bool, err error)

	// Authenticate checks the given password against the in-memory admin credential.
	Authenticate(ctx context.Context, password string) (bool, error)

	// CreateSession establishes a new authenticated session for the request,
	// writing the session cookie to w.
	CreateSession(w http.ResponseWriter, r *http.Request) error

	// ValidateSession reports whether r carries a valid authenticated session.
	ValidateSession(r *http.Request) (bool, error)

	// DeleteSession invalidates the session carried by r and clears its cookie.
	DeleteSession(w http.ResponseWriter, r *http.Request) error
}

type storeImpl struct {
	// passwordHash is the in-memory hash of the current instance admin credential,
	// set by Bootstrap. It is never written to the database.
	passwordHash string

	sessions *sessions.CookieStore
}

var _ Store = (*storeImpl)(nil)

func NewStore(key []byte) (Store, error) {
	cookieStore := sessions.NewCookieStore(key)
	cookieStore.Options = &sessions.Options{
		Path:     "/admin",
		MaxAge:   int(sessionDuration.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}

	return &storeImpl{sessions: cookieStore}, nil
}

func generatePassword() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *storeImpl) Bootstrap(
	ctx context.Context,
	presetPassword string,
) (string, bool, error) {
	generated := presetPassword == ""
	password := presetPassword
	if generated {
		var err error
		password, err = generatePassword()
		if err != nil {
			return "", false, err
		}
	}

	hash, err := argon2id.CreateHash(password, argon2id.DefaultParams)
	if err != nil {
		return "", false, err
	}
	s.passwordHash = hash

	if !generated {
		return "", false, nil
	}
	return password, true, nil
}

func (s *storeImpl) Authenticate(ctx context.Context, password string) (bool, error) {
	if s.passwordHash == "" {
		return false, nil
	}
	ok, err := argon2id.ComparePasswordAndHash(password, s.passwordHash)
	if errors.Is(err, argon2id.ErrInvalidHash) {
		return false, nil
	}
	return ok, err
}

func (s *storeImpl) CreateSession(w http.ResponseWriter, r *http.Request) error {
	session, err := s.sessions.New(r, sessionName)
	if err != nil {
		return err
	}
	session.Values[sessionAuthenticated] = true
	return s.sessions.Save(r, w, session)
}

func (s *storeImpl) ValidateSession(r *http.Request) (bool, error) {
	session, err := s.sessions.Get(r, sessionName)
	if err != nil {
		// Missing, expired, or tampered cookie - treat as unauthenticated rather
		// than as an error.
		return false, nil
	}
	authed, _ := session.Values[sessionAuthenticated].(bool)
	return authed, nil
}

func (s *storeImpl) DeleteSession(w http.ResponseWriter, r *http.Request) error {
	// Get always returns a usable (if empty) session, even on a decode error, so
	// there's nothing else to handle for an invalid/missing cookie here.
	session, _ := s.sessions.Get(r, sessionName)
	// Clearing Values (in addition to MaxAge, which only tells the browser to drop
	// the cookie) ensures the session is invalid even if a client replays the old
	// cookie value.
	session.Values[sessionAuthenticated] = false
	session.Options.MaxAge = -1
	return s.sessions.Save(r, w, session)
}
