package hive

import (
	"context"
	"regexp"
	"testing"

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

	fetchedID, err := h.LookupHandle(context.Background(), syntax.Handle("alice.org.example.com"))
	require.NoError(t, err)
	require.Equal(t, syntax.Handle("alice.org.example.com"), fetchedID.Handle)
	require.Equal(t, id.DID, fetchedID.DID)
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

func TestSignJWT(t *testing.T) {
	h := newTestHive(t, "example.com", "pear.example.com")

	ident, err := h.MintIdentity(context.Background(), "alice", "org")
	require.NoError(t, err)

	token, err := h.SignJWT(
		t.Context(),
		ident.DID,
		map[string]any{},
		jwt.MapClaims{
			"iss": ident.DID.String(),
		},
	)
	require.NoError(t, err)
	require.NotEmpty(t, token)
	key, err := ident.PublicKey()
	require.NoError(t, err)
	_, err = jwt.NewParser(jwt.WithIssuer(ident.DID.String())).
		Parse(token, func(token *jwt.Token) (any, error) {
			return key, nil
		})
	require.NoError(t, err)
}

func TestSignJWT_DIDNotFound(t *testing.T) {
	h := newTestHive(t, "example.com", "pear.example.com")
	_, err := h.SignJWT(
		context.Background(),
		syntax.DID("did:web:nonexist.example.com"),
		map[string]any{},
		jwt.MapClaims{
			"iss": "did:web:nonexist.example.com",
		},
	)
	require.ErrorIs(t, err, identity.ErrDIDNotFound)
}
