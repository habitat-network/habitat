package org

import (
	"context"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testSigningSecret = []byte("test-signing-secret-for-org-00000")

// fakeHive implements hive.Hive for org unit tests that need ResolveID/LookupID
// without spinning up a real hive store. Methods unused by org are left as
// panics so accidental reliance fails loudly.
type fakeHive struct {
	byID map[string]*identity.Identity
}

func newFakeHive(pairs ...struct {
	id  ID
	did syntax.DID
}) *fakeHive {
	f := &fakeHive{byID: map[string]*identity.Identity{}}
	for _, p := range pairs {
		f.byID[string(p.id)] = &identity.Identity{DID: p.did}
	}
	return f
}

func (f *fakeHive) ResolveID(did syntax.DID) (string, error) {
	for id, ident := range f.byID {
		if ident.DID == did {
			return id, nil
		}
	}
	return "", identity.ErrDIDNotFound
}

func (f *fakeHive) LookupID(_ context.Context, opaqueID string) (*identity.Identity, error) {
	if i, ok := f.byID[opaqueID]; ok {
		return i, nil
	}
	return nil, identity.ErrDIDNotFound
}

func (f *fakeHive) Lookup(_ context.Context, _ syntax.AtIdentifier) (*identity.Identity, error) {
	panic("fakeHive: Lookup not implemented")
}
func (f *fakeHive) LookupDID(_ context.Context, _ syntax.DID) (*identity.Identity, error) {
	panic("fakeHive: LookupDID not implemented")
}
func (f *fakeHive) LookupHandle(_ context.Context, _ syntax.Handle) (*identity.Identity, error) {
	panic("fakeHive: LookupHandle not implemented")
}
func (f *fakeHive) Purge(_ context.Context, _ syntax.AtIdentifier) error {
	panic("fakeHive: Purge not implemented")
}
func (f *fakeHive) MintIdentity(_ string) (*identity.Identity, string, func(*gorm.DB) error, error) {
	panic("fakeHive: MintIdentity not implemented")
}

var _ hive.Hive = (*fakeHive)(nil)

var (
	id1 = ID("test1")
	id2 = ID("test2")

	did1 = syntax.DID("did:web:test1.example.com")
	did2 = syntax.DID("did:web:test2.example.com")
)

const testPasswordHash = "testhash"
const testPassword = "test-password-123"

func newTestStore(t *testing.T) *store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	h := newFakeHive(
		struct {
			id  ID
			did syntax.DID
		}{id1, did1},
		struct {
			id  ID
			did syntax.DID
		}{id2, did2},
	)
	s, err := NewOrg("test-domain", h, db, testSigningSecret)
	require.NoError(t, err)
	return s
}

func newTestStoreWithHive(t *testing.T) *store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	h, err := hive.NewHive("example.com", "pear.example.com", db)
	require.NoError(t, err)
	s, err := NewOrg("test-domain", h, db, testSigningSecret)
	require.NoError(t, err)
	return s
}

func TestIsMember(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	ok, err := s.IsMember(ctx, did1)
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, s.addMember(ctx, id1, testPasswordHash))

	ok, err = s.IsMember(ctx, did1)

	require.NoError(t, err)
	require.True(t, ok)
}

func TestAddAdmin_GetAdmins(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	require.NoError(t, s.addMember(ctx, id1, testPasswordHash))
	require.NoError(t, s.AddAdmin(ctx, did1))

	admins, err := s.GetAdmins(ctx)
	require.NoError(t, err)
	require.Equal(t, []syntax.DID{did1}, admins)
}

func TestAddAdmin_NotMember(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	err := s.AddAdmin(ctx, did1)
	require.ErrorIs(t, err, ErrNotMember)
}

func TestRemoveAdmin_LastAdmin(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	require.NoError(t, s.addMember(ctx, id1, testPasswordHash))
	require.NoError(t, s.AddAdmin(ctx, did1))

	err := s.RemoveAdmin(ctx, did1)
	require.ErrorIs(t, err, ErrLastAdmin)
}

func TestRemoveAdmin_MultipleAdmins(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	require.NoError(t, s.addMember(ctx, id1, testPasswordHash))
	require.NoError(t, s.addMember(ctx, id2, testPasswordHash))
	require.NoError(t, s.AddAdmin(ctx, did1))
	require.NoError(t, s.AddAdmin(ctx, did2))

	require.NoError(t, s.RemoveAdmin(ctx, did2))

	admins, err := s.GetAdmins(ctx)
	require.NoError(t, err)
	require.Equal(t, []syntax.DID{did1}, admins)
}

func TestGetMembers(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	members, err := s.GetMembers(ctx)
	require.NoError(t, err)
	require.Empty(t, members)

	require.NoError(t, s.addMember(ctx, id1, testPasswordHash))
	require.NoError(t, s.addMember(ctx, id2, testPasswordHash))

	members, err = s.GetMembers(ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, []syntax.DID{did1, did2}, members)
}

func TestRemoveMembers(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	require.NoError(t, s.addMember(ctx, id1, testPasswordHash))
	require.NoError(t, s.addMember(ctx, id2, testPasswordHash))
	require.NoError(t, s.RemoveMembers(ctx, []syntax.DID{did2}))

	ok, err := s.IsMember(ctx, did1)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = s.IsMember(ctx, did2)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestGetMetadata(t *testing.T) {
	s := newTestStore(t)

	md := s.GetMetadata()
	require.Equal(t, md, habitat.NetworkHabitatOrgGetMetadataOutput{Domain: "test-domain"})
}

func TestGenerateAndUseIdentityToken(t *testing.T) {
	ctx := context.Background()
	s := newTestStoreWithHive(t)

	token, err := s.IssueIdentityToken(ctx, did1, false, time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.NotEmpty(t, token)

	_, err = s.CreateNewMemberIdentity(ctx, token, "alice", testPasswordHash)
	require.NoError(t, err)

	members, err := s.GetMembers(ctx)
	require.NoError(t, err)
	require.Len(t, members, 1)
}

func TestIdentityToken_CannotReuse(t *testing.T) {
	ctx := context.Background()
	s := newTestStoreWithHive(t)

	token, err := s.IssueIdentityToken(ctx, did1, false, time.Now().Add(time.Hour))
	require.NoError(t, err)

	_, err = s.CreateNewMemberIdentity(ctx, token, "alice", testPasswordHash)
	require.NoError(t, err)
	_, err = s.CreateNewMemberIdentity(ctx, token, "bob", testPasswordHash)
	require.ErrorIs(t, err, ErrInvalidToken)
}

func TestMintIdentity_DuplicateHandle(t *testing.T) {
	ctx := context.Background()
	s := newTestStoreWithHive(t)

	token1, err := s.IssueIdentityToken(ctx, did1, false, time.Now().Add(time.Hour))
	require.NoError(t, err)
	token2, err := s.IssueIdentityToken(ctx, did1, false, time.Now().Add(time.Hour))
	require.NoError(t, err)

	_, err = s.CreateNewMemberIdentity(ctx, token1, "alice", testPasswordHash)
	require.NoError(t, err)
	_, err = s.CreateNewMemberIdentity(ctx, token2, "alice", testPasswordHash)
	require.ErrorIs(t, err, hive.ErrNotCreated)
}

func TestIdentityToken_Reusable(t *testing.T) {
	ctx := context.Background()
	s := newTestStoreWithHive(t)

	token, err := s.IssueIdentityToken(ctx, did1, true, time.Now().Add(time.Hour))
	require.NoError(t, err)

	_, err = s.CreateNewMemberIdentity(ctx, token, "alice", testPasswordHash)
	require.NoError(t, err)
	_, err = s.CreateNewMemberIdentity(ctx, token, "bob", testPasswordHash)
	require.NoError(t, err)
	_, err = s.CreateNewMemberIdentity(ctx, token, "alice", testPasswordHash)
	require.ErrorIs(t, err, hive.ErrNotCreated)
}

func TestCreateNewMemberIdentity_AuthenticateMember(t *testing.T) {
	ctx := context.Background()
	s := newTestStoreWithHive(t)

	token, err := s.IssueIdentityToken(ctx, did1, false, time.Now().Add(time.Hour))
	require.NoError(t, err)

	_, err = s.CreateNewMemberIdentity(ctx, token, "alice", testPassword)
	require.NoError(t, err)

	ok, err := s.AuthenticateMember(ctx, "alice.example.com", testPassword)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = s.AuthenticateMember(ctx, "alice.example.com", "wrong-password")
	require.NoError(t, err)
	require.False(t, ok)

	ok, err = s.AuthenticateMember(ctx, "nobody.example.com", testPassword)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestIssueIdentityToken_ExpiryTooLate(t *testing.T) {
	ctx := context.Background()
	s := newTestStoreWithHive(t)

	_, err := s.IssueIdentityToken(ctx, did1, false, time.Now().AddDate(0, 1, 1))
	require.ErrorIs(t, err, ErrInvalidTokenExpiry)
}
