package login

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	jose "github.com/go-jose/go-jose/v3"
	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/utils"
	"gorm.io/gorm"
)

var errInvalidLoginToken = errors.New("invalid or expired login token")

var _ db.Store[*PasswordLoginProvider] = (*PasswordLoginProvider)(nil)

// PasswordLoginProvider wraps Store and implements login.Provider for habitat-hosted member identities.
type PasswordLoginProvider struct {
	db            *gorm.DB
	pearDomain    string
	signingSecret []byte
	dir           identity.Directory
}

type passwordEntry struct {
	DID      syntax.DID `gorm:"column:did;primaryKey"`
	Password string
}

func NewPasswordProvider(
	db *gorm.DB,
	pearDomain string,
	signingSecret []byte,
	dir identity.Directory,
) (*PasswordLoginProvider, error) {
	err := db.AutoMigrate(&passwordEntry{})
	if err != nil {
		return nil, err
	}
	return &PasswordLoginProvider{
		db:            db,
		pearDomain:    pearDomain,
		signingSecret: signingSecret,
		dir:           dir,
	}, nil
}

var _ Provider = (*PasswordLoginProvider)(nil)

func (p *PasswordLoginProvider) Authorize(
	ctx context.Context,
	loginHint string,
) (string, []byte, error) {
	// loginHint is the member's LoginID, which for password login is their DID
	// (see CreateNewMemberIdentity), not a human-readable handle. Resolve it to
	// a handle so the login page (typescript/apps/pear-pages) can display
	// something readable instead of a DID; fall back to the DID if resolution
	// fails.
	display := loginHint
	if did, err := syntax.ParseDID(loginHint); err == nil {
		if id, err := p.dir.LookupDID(ctx, did); err == nil && !id.Handle.IsInvalidHandle() {
			display = id.Handle.String()
		}
	}

	// The member login page is served by pear itself as a pre-rendered page
	// embedded under /ui/ (see internal/webui and typescript/apps/pear-pages).
	redirect := "https://" + p.pearDomain + "/ui/login/habitat?handle=" + url.QueryEscape(
		display,
	)
	return redirect, nil, nil
}

func (p *PasswordLoginProvider) issueToken(did syntax.DID) (string, error) {
	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: p.signingSecret}, nil)
	if err != nil {
		return "", err
	}
	return jwt.Signed(sig).Claims(jwt.Claims{
		Subject: did.String(),
		Expiry:  jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
	}).CompactSerialize()
}

func (p *PasswordLoginProvider) verifyToken(token string) (string, error) {
	parsed, err := jwt.ParseSigned(token)
	if err != nil {
		return "", fmt.Errorf("%w: %w", errInvalidLoginToken, err)
	}
	var claims jwt.Claims
	if err := parsed.Claims(p.signingSecret, &claims); err != nil {
		return "", fmt.Errorf("%w: %w", errInvalidLoginToken, err)
	}
	if err := claims.ValidateWithLeeway(jwt.Expected{Time: time.Now()}, 0); err != nil {
		return "", fmt.Errorf("%w: %w", errInvalidLoginToken, err)
	}
	return claims.Subject, nil
}

func (p *PasswordLoginProvider) Exchange(
	_ context.Context,
	query url.Values,
	_ []byte,
) (loginID string, err error) {
	return p.verifyToken(query.Get("code"))
}

func (p *PasswordLoginProvider) HandlePasswordLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req habitat.NetworkHabitatOrgLoginMemberInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	atid, err := syntax.ParseAtIdentifier(req.Handle)
	if err != nil {
		utils.LogAndHTTPError(ctx, w, err, "parsing at identifier", http.StatusBadRequest)
		return
	}

	id, err := p.dir.Lookup(ctx, atid)
	if err != nil {
		slog.ErrorContext(ctx, "invalid handle", "err", err)
		http.Error(w, "invalid handle", http.StatusUnauthorized)
		return
	}

	var entry passwordEntry
	if err := p.db.WithContext(ctx).Where("did = ?", id.DID).First(&entry).Error; err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	ok, err := verifyPassword(req.Password, entry.Password)
	if err != nil {
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"error while authenticating",
			http.StatusInternalServerError,
		)
		return
	}
	if !ok {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	token, err := p.issueToken(entry.DID)
	if err != nil {
		utils.LogAndHTTPError(ctx, w, err, "issuing token", http.StatusInternalServerError)
		return
	}

	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatOrgLoginMemberOutput{
		CallbackURL: "https://" + p.pearDomain + "/oauth-callback?code=" + token,
	})
}

func (p *PasswordLoginProvider) AddLoginEntry(did syntax.DID, password string) error {
	hash, err := hashPassword(password)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}
	return p.db.Create(&passwordEntry{
		DID:      did,
		Password: hash,
	}).Error
}

// WithTx implements [db.Store].
func (p *PasswordLoginProvider) WithTx(tx *gorm.DB) *PasswordLoginProvider {
	return &PasswordLoginProvider{
		db:            tx,
		dir:           p.dir,
		signingSecret: p.signingSecret,
		pearDomain:    p.pearDomain,
	}
}

// HashPassword hashes a plaintext password using argon2id and returns a
// self-describing encoded string that includes the parameters and salt.
func hashPassword(password string) (string, error) {
	return argon2id.CreateHash(password, argon2id.DefaultParams)
}

// VerifyPassword checks a plaintext password against an encoded argon2id hash.
// Returns false, nil on mismatch; error only if the hash is malformed.
func verifyPassword(password, encodedHash string) (bool, error) {
	ok, err := argon2id.ComparePasswordAndHash(password, encodedHash)
	if errors.Is(err, argon2id.ErrInvalidHash) {
		return false, nil
	}
	return ok, err
}
