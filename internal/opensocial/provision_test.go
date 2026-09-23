package opensocial_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func newOrgWithoutCreator(t *testing.T, s *opensocial_testutil.TestStore) syntax.DID {
	t.Helper()
	orgDIDStr, err := s.NewOrgWithoutCreator(t.Context(), "acme")
	require.NoError(t, err)
	return syntax.DID(orgDIDStr)
}

func TestStoreProvisionMember(t *testing.T) {
	s := opensocial_testutil.NewTestStore(t)
	org := newOrgWithoutCreator(t, s)
	membersSpace := habitat_syntax.ConstructSpaceURI(org, opensocial.MembersSpaceType, "self")
	first := syntax.DID("did:plc:first")
	second := syntax.DID("did:plc:second")

	// The first member of a creator-less org becomes its admin...
	require.NoError(t, s.ProvisionMember(t.Context(), org, first))
	roles, err := s.GetUserRoles(t.Context(), org, first)
	require.NoError(t, err)
	require.Equal(t, []string{opensocial.AdminRoleRkey}, roles)

	// ...with an acceptance record authored under their own repo, so the org
	// shows up in their member spaces.
	_, err = s.SpaceStore.GetRecord(
		t.Context(), membersSpace, first, opensocial.AcceptanceCollection, "self",
	)
	require.NoError(t, err)
	memberSpaces, err := s.ListMemberSpaces(t.Context(), first)
	require.NoError(t, err)
	require.Contains(t, memberSpaces, membersSpace)

	// Everyone after is a plain member.
	require.NoError(t, s.ProvisionMember(t.Context(), org, second))
	roles, err = s.GetUserRoles(t.Context(), org, second)
	require.NoError(t, err)
	require.Equal(t, []string{opensocial.MemberRoleRkey}, roles)
	roles, err = s.GetUserRoles(t.Context(), org, first)
	require.NoError(t, err)
	require.Equal(t, []string{opensocial.AdminRoleRkey}, roles)
}

func TestStoreProvisionMemberConcurrentFirstSignIns(t *testing.T) {
	s := opensocial_testutil.NewTestStore(t)
	org := newOrgWithoutCreator(t, s)

	const n = 5
	members := make([]syntax.DID, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		members[i] = syntax.DID(fmt.Sprintf("did:plc:member%d", i))
		wg.Go(func() { errs[i] = s.ProvisionMember(t.Context(), org, members[i]) })
	}
	wg.Wait()

	admins := 0
	for i, member := range members {
		require.NoError(t, errs[i])
		roles, err := s.GetUserRoles(t.Context(), org, member)
		require.NoError(t, err)
		if slices.Equal(roles, []string{opensocial.AdminRoleRkey}) {
			admins++
		} else {
			require.Equal(t, []string{opensocial.MemberRoleRkey}, roles)
		}
	}
	require.Equal(t, 1, admins)
}

// failAcceptanceStore is a spaces.Store that fails every write to the
// acceptance collection, to exercise ProvisionMember's rollback.
type failAcceptanceStore struct {
	spaces.Store
}

func (f failAcceptanceStore) WithTx(tx *gorm.DB) spaces.Store {
	return failAcceptanceStore{f.Store.WithTx(tx)}
}

func (f failAcceptanceStore) PutRecord(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	owner syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
	value spaces.MarshaledRecord,
) (habitat_syntax.SpaceRecordURI, *cid.Cid, error) {
	if collection == opensocial.AcceptanceCollection {
		return "", nil, errors.New("acceptance write failed")
	}
	return f.Store.PutRecord(ctx, space, owner, collection, rkey, value)
}

func TestStoreProvisionMemberRollsBack(t *testing.T) {
	base := opensocial_testutil.NewTestStore(t)
	org := newOrgWithoutCreator(t, base)
	failing := opensocial_testutil.NewTestStore(
		t,
		opensocial_testutil.WithDB(base.DB),
		opensocial_testutil.WithHive(base.Hive),
		opensocial_testutil.WithSpaceStore(failAcceptanceStore{base.SpaceStore}),
	)
	member := syntax.DID("did:plc:member")

	require.Error(t, failing.ProvisionMember(t.Context(), org, member))

	// The membership write was rolled back along with the failed acceptance.
	roles, err := base.GetUserRoles(t.Context(), org, member)
	require.NoError(t, err)
	require.Empty(t, roles)
}
