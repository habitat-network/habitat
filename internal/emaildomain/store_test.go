package emaildomain_test

import (
	"errors"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/emaildomain"
)

// TestStore runs against one store shared by the subtests, in order: the
// member-email subtests rely on the mapping created by "domain mapping".
func TestStore(t *testing.T) {
	s, err := emaildomain.NewStore(db_testutil.NewDB(t))
	require.NoError(t, err)
	org := syntax.DID("did:web:acme.example.com")
	alice := syntax.DID("did:web:alice.example.com")
	aliceEmail := emaildomain.Email("alice@acme.com")

	t.Run("domain mapping", func(t *testing.T) {
		// Domains are stored lowercased.
		require.NoError(t, s.CreateDomainMapping(
			t.Context(), "ACME.com", org, emaildomain.LoginMethodGoogle,
		))
		gotOrg, method, ok, err := s.LookupDomain(t.Context(), "acme.com")
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, org, gotOrg)
		require.Equal(t, emaildomain.LoginMethodGoogle, method)

		// Two domains may map to the same org...
		require.NoError(t, s.CreateDomainMapping(
			t.Context(), "acme.io", org, emaildomain.LoginMethodGoogle,
		))
		// ...but one domain can't map to two orgs.
		err = s.CreateDomainMapping(
			t.Context(), "acme.com", "did:web:other.example.com", emaildomain.LoginMethodGoogle,
		)
		require.ErrorIs(t, err, emaildomain.ErrDomainTaken)

		_, _, ok, err = s.LookupDomain(t.Context(), "unknown.com")
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("member emails", func(t *testing.T) {
		_, ok, err := s.GetDID(t.Context(), aliceEmail)
		require.NoError(t, err)
		require.False(t, ok)
		_, ok, err = s.GetLoginMethod(t.Context(), alice)
		require.NoError(t, err)
		require.False(t, ok)

		require.NoError(t, s.Provision(t.Context(), aliceEmail, org, alice))

		did, ok, err := s.GetDID(t.Context(), aliceEmail)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, alice, did)

		email, ok, err := s.GetEmail(t.Context(), alice)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, aliceEmail, email)

		method, ok, err := s.GetLoginMethod(t.Context(), alice)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, emaildomain.LoginMethodGoogle, method)

		err = s.Provision(t.Context(), aliceEmail, org, "did:web:alice2.example.com")
		require.ErrorIs(t, err, emaildomain.ErrEmailProvisioned)
	})

	t.Run("transaction rolls back", func(t *testing.T) {
		bobEmail := emaildomain.Email("bob@acme.com")
		err := s.Transaction(t.Context(), func(tx *gorm.DB) error {
			require.NoError(t, s.WithTx(tx).Provision(
				t.Context(), bobEmail, org, "did:web:bob.example.com",
			))
			return errors.New("roll back")
		})
		require.Error(t, err)
		_, ok, err := s.GetDID(t.Context(), bobEmail)
		require.NoError(t, err)
		require.False(t, ok)
	})
}
