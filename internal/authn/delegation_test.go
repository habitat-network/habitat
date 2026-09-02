package authn_test

import (
	"crypto/ecdsa"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/authn"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/did"
	"github.com/habitat-network/habitat/internal/fgastore"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
	"github.com/habitat-network/habitat/internal/perms"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/stretchr/testify/require"
)

// newDelegationRequest builds a GET "/" request presenting token as a
// delegation token, with a DPoP proof (no `ath`: a delegation token is a
// grant, not the DPoP-bound artifact) proving possession of key.
func newDelegationRequest(t *testing.T, token string, key *ecdsa.PrivateKey) *http.Request {
	t.Helper()
	r := httptest.NewRequest("GET", "/", http.NoBody)
	r.Header.Set("Authorization", "Bearer "+token)
	proof, err := utils.SignDPoPProof(key, http.MethodGet, "http://"+r.Host+r.URL.Path, "")
	require.NoError(t, err)
	r.Header.Set("DPoP", proof)
	return r
}

// newTestPermsStore returns a perms.Store backed by an in-memory fga store,
// along with the spaces.Store backing it, so tests grant/deny space roles
// through the store's own methods rather than writing fga tuples directly.
// SetUserRelation persists a relationship record into the space itself, so
// tests exercising it need the space to actually exist first (see
// newTestSpace).
func newTestPermsStore(t *testing.T) (perms.Store, spaces.Store) {
	t.Helper()
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })

	db := db_testutil.NewDB(t)
	sp := spaces_testutil.NewTestStore(t, spaces_testutil.WithDB(db), spaces_testutil.WithFGA(fga))
	os := opensocial_testutil.NewTestStore(
		t,
		opensocial_testutil.WithDB(db),
		opensocial_testutil.WithSpaceStore(sp),
	)
	return perms.NewStore(db, sp, fga, os), sp
}

// newTestSpace creates the space used throughout this test's subtests,
// owned by space.SpaceOwner(), in sp.
func newTestSpace(t *testing.T, sp spaces.Store, space habitat_syntax.SpaceURI) {
	t.Helper()
	owner := space.SpaceOwner()
	_, err := sp.CreateSpace(t.Context(), owner, space.SpaceType(), space.Skey())
	require.NoError(t, err)
}

type testDelegatorOptions struct {
	dir     identity.Directory
	srv     authn.SpaceRoleValidator
	hostKey atcrypto.PrivateKey
}

type Option func(*testDelegatorOptions)

func WithDirectory(dir identity.Directory) Option {
	return func(o *testDelegatorOptions) {
		o.dir = dir
	}
}

func WithSpaceRoleValidator(srv authn.SpaceRoleValidator) Option {
	return func(o *testDelegatorOptions) {
		o.srv = srv
	}
}

func WithHostKey(key atcrypto.PrivateKey) Option {
	return func(o *testDelegatorOptions) {
		o.hostKey = key
	}
}

// newTestDelegator builds a DelegationTokenAuthMethod backed by a fresh
// perms.Store, letting individual tests override the directory, permission
// store, or host key.
func newTestDelegator(t *testing.T, opts ...Option) *authn.DelegationTokenAuthMethod {
	t.Helper()
	store, _ := newTestPermsStore(t)
	options := &testDelegatorOptions{
		dir: identity.NewMockDirectory(),
		srv: store,
	}
	for _, o := range opts {
		o(options)
	}
	return authn.NewDelegationTokenAuthMethod(options.dir, options.srv, options.hostKey)
}

func TestDelegationAuthMethod(t *testing.T) {
	userKey, _ := atcrypto.GeneratePrivateKeyK256()
	userPubKey, _ := userKey.PublicKey()
	dir := identity.NewMockDirectory()
	dir.Insert(*did.Web("pear.com").AtprotoKey(userPubKey.Multibase()).Build())
	// Owned by a different DID than the acting user below, so that
	// perms.Store's implicit space-owner grant doesn't mask the
	// "no permission" cases.
	space := habitat_syntax.SpaceURI("at://did:web:space-owner.com/space/com.test.space/abc")
	user := syntax.DID("did:web:pear.com")

	t.Run("can handle", func(t *testing.T) {
		token, err := utils.DelegationToken(userKey, user, "#atproto", space)
		require.NoError(t, err)
		r := newAuthenticatedRequest(token)
		require.True(t, newTestDelegator(t, WithDirectory(dir)).CanHandle(r))
	})

	t.Run("has permission", func(t *testing.T) {
		store, sp := newTestPermsStore(t)
		newTestSpace(t, sp, space)
		_, err := store.SetUserRelation(t.Context(), user, space, habitat_syntax.SpaceRoleReader)
		require.NoError(t, err)
		token, err := utils.DelegationToken(userKey, user, "#atproto", space)
		require.NoError(t, err)
		dpopKey, err := utils.GenerateDPoPKey()
		require.NoError(t, err)
		r := newDelegationRequest(t, token, dpopKey)
		credInfo, ok := newTestDelegator(t, WithDirectory(dir), WithSpaceRoleValidator(store)).
			Validate(httptest.NewRecorder(), r)
		require.True(t, ok)
		require.Equal(
			t, credInfo, &authn.CredentialInfo{Space: space, DPoPJKT: jwkThumbprint(t, dpopKey)},
		)
	})

	t.Run("no permission", func(t *testing.T) {
		token, err := utils.DelegationToken(userKey, user, "#atproto", space)
		require.NoError(t, err)
		dpopKey, err := utils.GenerateDPoPKey()
		require.NoError(t, err)
		r := newDelegationRequest(t, token, dpopKey)
		_, ok := newTestDelegator(t, WithDirectory(dir)).Validate(httptest.NewRecorder(), r)
		require.False(t, ok)
	})

	t.Run("host key has permission", func(t *testing.T) {
		hostKey, _ := atcrypto.GeneratePrivateKeyK256()
		store, sp := newTestPermsStore(t)
		newTestSpace(t, sp, space)
		_, err := store.SetUserRelation(t.Context(), user, space, habitat_syntax.SpaceRoleReader)
		require.NoError(t, err)
		token, err := utils.DelegationToken(hostKey, user, "#habitat", space)
		require.NoError(t, err)
		dpopKey, err := utils.GenerateDPoPKey()
		require.NoError(t, err)
		r := newDelegationRequest(t, token, dpopKey)
		credInfo, ok := newTestDelegator(
			t,
			WithDirectory(dir),
			WithSpaceRoleValidator(store),
			WithHostKey(hostKey),
		).Validate(httptest.NewRecorder(), r)
		require.True(t, ok)
		require.Equal(
			t, credInfo, &authn.CredentialInfo{Space: space, DPoPJKT: jwkThumbprint(t, dpopKey)},
		)
	})

	t.Run("host key no permission", func(t *testing.T) {
		hostKey, _ := atcrypto.GeneratePrivateKeyK256()
		token, err := utils.DelegationToken(hostKey, user, "#habitat", space)
		require.NoError(t, err)
		dpopKey, err := utils.GenerateDPoPKey()
		require.NoError(t, err)
		r := newDelegationRequest(t, token, dpopKey)
		_, ok := newTestDelegator(t, WithDirectory(dir), WithHostKey(hostKey)).
			Validate(httptest.NewRecorder(), r)
		require.False(t, ok)
	})

	t.Run("no token", func(t *testing.T) {
		_, ok := newTestDelegator(t, WithDirectory(dir)).Validate(
			httptest.NewRecorder(),
			httptest.NewRequest("GET", "/", http.NoBody),
		)
		require.False(t, ok)
	})

	t.Run("invalid signature", func(t *testing.T) {
		otherKey, _ := atcrypto.GeneratePrivateKeyK256()
		store, sp := newTestPermsStore(t)
		newTestSpace(t, sp, space)
		_, err := store.SetUserRelation(t.Context(), user, space, habitat_syntax.SpaceRoleReader)
		require.NoError(t, err)
		token, err := utils.DelegationToken(otherKey, user, "#atproto", space)
		require.NoError(t, err)
		dpopKey, err := utils.GenerateDPoPKey()
		require.NoError(t, err)
		r := newDelegationRequest(t, token, dpopKey)
		credInfo, ok := newTestDelegator(t, WithDirectory(dir), WithSpaceRoleValidator(store)).
			Validate(httptest.NewRecorder(), r)
		require.False(t, ok)
		require.Nil(t, credInfo)
	})

	t.Run("missing DPoP proof", func(t *testing.T) {
		store, sp := newTestPermsStore(t)
		newTestSpace(t, sp, space)
		_, err := store.SetUserRelation(t.Context(), user, space, habitat_syntax.SpaceRoleReader)
		require.NoError(t, err)
		token, err := utils.DelegationToken(userKey, user, "#atproto", space)
		require.NoError(t, err)
		r := newAuthenticatedRequest(token)
		credInfo, ok := newTestDelegator(t, WithDirectory(dir), WithSpaceRoleValidator(store)).
			Validate(httptest.NewRecorder(), r)
		require.False(t, ok)
		require.Nil(t, credInfo)
	})
}
