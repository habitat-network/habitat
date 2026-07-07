package hive

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/stretchr/testify/require"
)

func newTestHive(t *testing.T, memberDomain, pearDomain string) Hive {
	t.Helper()
	h, err := NewHive(memberDomain, pearDomain, testutil.NewDB(t))
	require.NoError(t, err)
	return h
}

func TestMintIdentity(t *testing.T) {
	h := newTestHive(t, "example.com", "pear.example.com")
	id, err := h.MintIdentity(context.Background(), "alice", "org")
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile("did:web:.*.example.com"), id.DID.String())
	require.Equal(t, "alice.org.example.com", id.Handle.String())
}

func TestMintIdentity_InvalidHandle(t *testing.T) {
	h := newTestHive(t, "example.com", "pear.example.com")
	_, err := h.MintIdentity(context.Background(), "alice!invalid", "org")
	require.ErrorIs(t, err, identity.ErrInvalidHandle)
}

func TestMintIdentity_Duplicate(t *testing.T) {
	h := newTestHive(t, "example.com", "pear.example.com")
	_, err := h.MintIdentity(context.Background(), "alice", "org")
	require.NoError(t, err)
	_, err = h.MintIdentity(context.Background(), "alice", "org")
	require.ErrorIs(t, err, ErrNotCreated)
}

func TestLookupHandle(t *testing.T) {
	h := newTestHive(t, "example.com", "pear.example.com")

	id, err := h.MintIdentity(context.Background(), "alice", "org")
	require.NoError(t, err)

	fetchedId, err := h.LookupHandle(context.Background(), syntax.Handle("alice.org.example.com"))
	require.NoError(t, err)
	require.Equal(t, syntax.Handle("alice.org.example.com"), fetchedId.Handle)
	require.Equal(t, id.DID, fetchedId.DID)
}

func TestLookupHandle_NotFound(t *testing.T) {
	h := newTestHive(t, "example.com", "pear.example.com")

	_, err := h.LookupHandle(context.Background(), syntax.Handle("nobody.org.example.com"))
	require.ErrorIs(t, err, identity.ErrHandleNotFound)
}

func TestLookupHandle_WrongDomain(t *testing.T) {
	h := newTestHive(t, "example.com", "pear.example.com")
	_, err := h.MintIdentity(context.Background(), "alice", "org")
	require.NoError(t, err)
	_, err = h.LookupHandle(context.Background(), syntax.Handle("alice.org.other.com"))
	require.ErrorIs(t, err, identity.ErrHandleNotFound)
}

func TestLookupDID(t *testing.T) {
	h := newTestHive(t, "example.com", "pear.example.com")

	_, err := h.MintIdentity(context.Background(), "alice", "org")
	require.NoError(t, err)

	ident, err := h.LookupHandle(context.Background(), syntax.Handle("alice.org.example.com"))
	require.NoError(t, err)

	ident2, err := h.LookupDID(context.Background(), ident.DID)
	require.NoError(t, err)
	require.Equal(t, ident.DID, ident2.DID)
	require.Equal(t, ident.Handle, ident2.Handle)
}

func TestLookupDID_NotFound(t *testing.T) {
	h := newTestHive(t, "example.com", "pear.example.com")

	_, err := h.LookupDID(context.Background(), syntax.DID("did:web:xxxxxx.example.com"))
	require.ErrorIs(t, err, identity.ErrDIDNotFound)
}

func TestLookupDID_PLC(t *testing.T) {
	h := newTestHive(t, "example.com", "pear.example.com")
	_, err := h.LookupDID(t.Context(), syntax.DID("did:plc:abc123"))
	require.ErrorIs(t, err, identity.ErrDIDNotFound)
}

func TestSignServiceAuth(t *testing.T) {
	h := newTestHive(t, "example.com", "pear.example.com")

	_, err := h.MintIdentity(context.Background(), "alice", "org")
	require.NoError(t, err)

	ident, err := h.LookupHandle(context.Background(), syntax.Handle("alice.org.example.com"))
	require.NoError(t, err)

	lxm := syntax.NSID("com.atproto.repo.createRecord")
	token, err := h.SignServiceAuth(
		context.Background(),
		ident.DID,
		"did:web:pear.example.com",
		time.Hour,
		&lxm,
	)
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
	h := newTestHive(t, "example.com", "pear.example.com")

	_, err := h.SignServiceAuth(
		context.Background(),
		syntax.DID("did:web:nonexist.example.com"),
		"did:web:pear.example.com",
		time.Hour,
		nil,
	)
	require.ErrorIs(t, err, identity.ErrDIDNotFound)
}

func TestSignServiceAuth_WrongDomain(t *testing.T) {
	h := newTestHive(t, "example.com", "pear.example.com")

	_, err := h.SignServiceAuth(
		context.Background(),
		syntax.DID("did:web:nonexist.other.com"),
		"did:web:pear.example.com",
		time.Hour,
		nil,
	)
	require.ErrorIs(t, err, identity.ErrDIDNotFound)
}
