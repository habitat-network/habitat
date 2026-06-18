package instanceadmin

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/alexedwards/argon2id"
	jose "github.com/go-jose/go-jose/v3"
	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/gorilla/sessions"
	"gorm.io/gorm"
)

const (
	sessionName          = "habitat_admin_session"
	sessionDuration      = 24 * time.Hour
	sessionAuthenticated = "authenticated"
	inviteDuration       = 7 * 24 * time.Hour
	policyOpen           = "open"
	policyInviteOnly     = "invite_only"
)

var ErrInvalidPolicy = errors.New("invalid org creation policy")
var ErrInvalidInvite = errors.New("invalid or expired invite")

// Store manages the instance admin credential and its sessions. Neither is
// persisted: the credential lives only in process memory (see Bootstrap), and
// sessions are encoded directly into a signed/encrypted cookie via
// gorilla/sessions, so both reset on restart.
type Store interface {
	// Authenticate checks the given password against the in-memory admin credential.
	Authenticate(ctx context.Context, password string) (bool, error)

	// CreateSession establishes a new authenticated session for the request,
	// writing the session cookie to w.
	CreateSession(w http.ResponseWriter, r *http.Request) error

	// ValidateSession reports whether r carries a valid authenticated session.
	ValidateSession(r *http.Request) (bool, error)

	// DeleteSession invalidates the session carried by r and clears its cookie.
	DeleteSession(w http.ResponseWriter, r *http.Request) error

	// GetSettings returns the instance's current name and org creation
	// policy, creating the settings row with defaults on first access.
	GetSettings(ctx context.Context) (instanceName, orgCreationPolicy string, err error)

	// UpdateSettings updates the instance's name and org creation policy.
	// orgCreationPolicy must be "open" or "invite_only".
	UpdateSettings(ctx context.Context, instanceName, orgCreationPolicy string) error

	// GetOrgCreationPolicy is a convenience accessor for just the policy field.
	GetOrgCreationPolicy(ctx context.Context) (string, error)

	// IssueInvite creates and signs a new single-use invite token.
	IssueInvite(ctx context.Context) (token string, err error)

	// ValidateInvite checks that token is a well-formed, signed, unexpired,
	// not-yet-used invite for this instance, without consuming it. All
	// failure modes (unknown, expired, already used, bad signature) return
	// ErrInvalidInvite.
	ValidateInvite(ctx context.Context, token string) error

	// MarkInviteUsed marks the invite identified by token as used. Call this
	// only after the action the invite was gating (e.g. org creation) has
	// actually succeeded. token must have already passed ValidateInvite.
	MarkInviteUsed(ctx context.Context, token string) error
}

type storeImpl struct {
	// passwordHash is the in-memory hash of the current instance admin credential,
	// set by Bootstrap. It is never written to the database.
	passwordHash string

	sessions *sessions.CookieStore
	db       *gorm.DB
	domain   string
}

var _ Store = (*storeImpl)(nil)

func NewStore(db *gorm.DB, key []byte, domain, passwordHash string) (Store, error) {
	cookieStore := sessions.NewCookieStore(key)
	cookieStore.Options = &sessions.Options{
		Path:     "/admin",
		MaxAge:   int(sessionDuration.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
	if err := db.AutoMigrate(&instanceSettings{}); err != nil {
		return nil, err
	}

	return &storeImpl{
		passwordHash: passwordHash,
		sessions:     cookieStore,
		domain:       domain,
		db:           db,
	}, nil
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

// getOrCreateSettings lazily creates the singleton settings row on first access.
// This assumes a single pear process per instance — concurrent first-accesses from
// multiple processes against the same database could race on the initial Create.
func (s *storeImpl) getOrCreateSettings(ctx context.Context) (*instanceSettings, error) {
	var settings instanceSettings
	err := s.db.WithContext(ctx).First(&settings, instanceSettingsID).Error
	if err == nil {
		return &settings, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	secret, err := generateSigningSecret()
	if err != nil {
		return nil, err
	}
	settings = instanceSettings{
		ID:                instanceSettingsID,
		OrgCreationPolicy: policyOpen,
		SigningSecret:     secret,
		CreatedAt:         time.Now(),
	}
	if err := s.db.WithContext(ctx).Create(&settings).Error; err != nil {
		return nil, err
	}
	return &settings, nil
}

func generateSigningSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func (s *storeImpl) GetSettings(ctx context.Context) (string, string, error) {
	settings, err := s.getOrCreateSettings(ctx)
	if err != nil {
		return "", "", err
	}
	return settings.InstanceName, settings.OrgCreationPolicy, nil
}

func (s *storeImpl) UpdateSettings(ctx context.Context, instanceName, orgCreationPolicy string) error {
	if orgCreationPolicy != policyOpen && orgCreationPolicy != policyInviteOnly {
		return ErrInvalidPolicy
	}
	if _, err := s.getOrCreateSettings(ctx); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&instanceSettings{}).
		Where("id = ?", instanceSettingsID).
		Updates(map[string]any{
			"instanceName":      instanceName,
			"orgCreationPolicy": orgCreationPolicy,
		}).Error
}

func (s *storeImpl) GetOrgCreationPolicy(ctx context.Context) (string, error) {
	settings, err := s.getOrCreateSettings(ctx)
	if err != nil {
		return "", err
	}
	return settings.OrgCreationPolicy, nil
}

type inviteClaims struct {
	jwt.Claims
	Domain string `json:"domain"`
	Name   string `json:"name"`
}

func (s *storeImpl) IssueInvite(ctx context.Context) (string, error) {
	settings, err := s.getOrCreateSettings(ctx)
	if err != nil {
		return "", err
	}

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	jti := hex.EncodeToString(b)
	now := time.Now()
	expiresAt := now.Add(inviteDuration)

	invite := instanceInvite{
		Token:     jti,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}
	if err := s.db.WithContext(ctx).Create(&invite).Error; err != nil {
		return "", err
	}

	secret, err := base64.StdEncoding.DecodeString(settings.SigningSecret)
	if err != nil {
		return "", err
	}
	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: secret}, nil)
	if err != nil {
		return "", err
	}
	return jwt.Signed(sig).Claims(inviteClaims{
		Claims: jwt.Claims{
			ID:     jti,
			Expiry: jwt.NewNumericDate(expiresAt),
		},
		Domain: s.domain,
		Name:   settings.InstanceName,
	}).CompactSerialize()
}

// checkInvite parses+verifies the JWT (signature via the instance's
// SigningSecret, expiry) and loads the corresponding instanceInvite DB row,
// confirming it exists, is unexpired, and is not yet used. It does not mutate
// any state. All failure modes collapse into ErrInvalidInvite.
func (s *storeImpl) checkInvite(ctx context.Context, token string) (*instanceInvite, error) {
	settings, err := s.getOrCreateSettings(ctx)
	if err != nil {
		return nil, err
	}
	secret, err := base64.StdEncoding.DecodeString(settings.SigningSecret)
	if err != nil {
		return nil, err
	}

	parsed, err := jwt.ParseSigned(token)
	if err != nil {
		return nil, ErrInvalidInvite
	}
	var claims inviteClaims
	if err := parsed.Claims(secret, &claims); err != nil {
		return nil, ErrInvalidInvite
	}
	if err := claims.Claims.ValidateWithLeeway(jwt.Expected{Time: time.Now()}, 0); err != nil {
		return nil, ErrInvalidInvite
	}

	var invite instanceInvite
	if err := s.db.WithContext(ctx).Where("token = ?", claims.ID).First(&invite).Error; err != nil {
		return nil, ErrInvalidInvite
	}
	if invite.UsedAt != nil || invite.ExpiresAt.Before(time.Now()) {
		return nil, ErrInvalidInvite
	}

	return &invite, nil
}

func (s *storeImpl) ValidateInvite(ctx context.Context, token string) error {
	_, err := s.checkInvite(ctx, token)
	return err
}

func (s *storeImpl) MarkInviteUsed(ctx context.Context, token string) error {
	invite, err := s.checkInvite(ctx, token)
	if err != nil {
		return err
	}

	now := time.Now()
	return s.db.WithContext(ctx).Model(&instanceInvite{}).
		Where("token = ?", invite.Token).
		Update("used_at", &now).Error
}
