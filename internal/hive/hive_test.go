package hive

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestHive(t *testing.T, memberDomain, pearDomain string) (Hive, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	h, err := NewHive(memberDomain, pearDomain, db)
	require.NoError(t, err)
	return h, db
}

// mintAndPersist is a test helper that mints an identity and persists it atomically.
func mintAndPersist(t *testing.T, h Hive, db *gorm.DB, handle string) {
	t.Helper()
	_, persist, err := h.MintIdentity(handle)
	require.NoError(t, err)
	require.NoError(t, persist(db))
}

func TestMintIdentity(t *testing.T) {
	h, db := newTestHive(t, "example.com", "pear.example.com")
	mintAndPersist(t, h, db, "alice")
}

func TestMintIdentity_InvalidHandle(t *testing.T) {
	h, _ := newTestHive(t, "example.com", "pear.example.com")
	_, _, err := h.MintIdentity("alice!invalid")
	require.ErrorIs(t, err, identity.ErrInvalidHandle)
}

func TestMintIdentity_Duplicate(t *testing.T) {
	h, db := newTestHive(t, "example.com", "pear.example.com")

	mintAndPersist(t, h, db, "alice")

	_, persist, err := h.MintIdentity("alice")
	require.NoError(t, err)
	require.ErrorIs(t, persist(db), ErrNotCreated)
}

func TestLookupHandle(t *testing.T) {
	h, db := newTestHive(t, "example.com", "pear.example.com")

	mintAndPersist(t, h, db, "alice")

	ident, err := h.LookupHandle(context.Background(), syntax.Handle("alice.example.com"))
	require.NoError(t, err)
	require.Equal(t, syntax.Handle("alice.example.com"), ident.Handle)
	require.True(t, strings.HasPrefix(ident.DID.String(), "did:web:"))
}

func TestLookupHandle_NotFound(t *testing.T) {
	h, _ := newTestHive(t, "example.com", "pear.example.com")

	_, err := h.LookupHandle(context.Background(), syntax.Handle("nobody.example.com"))
	require.ErrorIs(t, err, identity.ErrHandleNotFound)
}

func TestLookupHandle_WrongDomain(t *testing.T) {
	h, db := newTestHive(t, "example.com", "pear.example.com")

	mintAndPersist(t, h, db, "alice")

	_, err := h.LookupHandle(context.Background(), syntax.Handle("alice.other.com"))
	require.ErrorIs(t, err, identity.ErrHandleNotFound)
}

func TestLookupDID(t *testing.T) {
	h, db := newTestHive(t, "example.com", "pear.example.com")

	mintAndPersist(t, h, db, "alice")

	ident, err := h.LookupHandle(context.Background(), syntax.Handle("alice.example.com"))
	require.NoError(t, err)

	ident2, err := h.LookupDID(context.Background(), ident.DID)
	require.NoError(t, err)
	require.Equal(t, ident.DID, ident2.DID)
	require.Equal(t, ident.Handle, ident2.Handle)
}

func TestLookupDID_NotFound(t *testing.T) {
	h, _ := newTestHive(t, "example.com", "pear.example.com")

	_, err := h.LookupDID(context.Background(), syntax.DID("did:web:xxxxxx.example.com"))
	require.ErrorIs(t, err, identity.ErrDIDNotFound)
}

func TestSignServiceAuth(t *testing.T) {
	h, db := newTestHive(t, "example.com", "pear.example.com")

	mintAndPersist(t, h, db, "alice")

	ident, err := h.LookupHandle(context.Background(), syntax.Handle("alice.example.com"))
	require.NoError(t, err)

	lxm := syntax.NSID("com.atproto.repo.createRecord")
	token, err := h.SignServiceAuth(context.Background(), ident.DID, "did:web:pear.example.com", time.Hour, &lxm)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	p := jwt.NewParser(jwt.WithoutClaimsValidation())
	claims := &jwt.MapClaims{}
	_, _, err = p.ParseUnverified(token, claims)
	require.NoError(t, err)

	iss, err := claims.GetIssuer()
	require.NoError(t, err)
	require.Equal(t, ident.DID.String(), iss)

	aud, err := claims.GetAudience()
	require.NoError(t, err)
	require.Len(t, aud, 1)
	require.Equal(t, "did:web:pear.example.com", aud[0])

	require.Equal(t, "com.atproto.repo.createRecord", (*claims)["lxm"])

	exp, err := claims.GetExpirationTime()
	require.NoError(t, err)
	require.NotZero(t, exp.Time)
	require.WithinRange(t, exp.Time, time.Now().Add(59*time.Minute), time.Now().Add(61*time.Minute))

	iat, err := claims.GetIssuedAt()
	require.NoError(t, err)
	require.NotZero(t, iat.Time)
	require.WithinRange(t, iat.Time, time.Now().Add(-time.Minute), time.Now().Add(time.Minute))

	jti, ok := (*claims)["jti"]
	require.True(t, ok)
	jtiStr, ok := jti.(string)
	require.True(t, ok)
	require.NotEmpty(t, jtiStr)
}

func TestSignServiceAuth_DIDNotFound(t *testing.T) {
	h, _ := newTestHive(t, "example.com", "pear.example.com")

	_, err := h.SignServiceAuth(context.Background(), syntax.DID("did:web:nonexist.example.com"), "did:web:pear.example.com", time.Hour, nil)
	require.ErrorIs(t, err, identity.ErrDIDNotFound)
}

func TestSignServiceAuth_WrongDomain(t *testing.T) {
	h, _ := newTestHive(t, "example.com", "pear.example.com")

	_, err := h.SignServiceAuth(context.Background(), syntax.DID("did:web:nonexist.other.com"), "did:web:pear.example.com", time.Hour, nil)
	require.ErrorIs(t, err, identity.ErrDIDNotFound)
}
