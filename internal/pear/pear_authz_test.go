package pear

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/stretchr/testify/require"
)

// TestHasPermission tests the HasPermission pear method's authz logic.
// The rule: the caller must itself have permission to the record in order to
// check whether any requester (including itself) has permission.
func TestHasPermission(t *testing.T) {
	ownerDID := syntax.DID("did:plc:owner")
	granteeDID := syntax.DID("did:plc:grantee")
	nonGranteeDID := syntax.DID("did:plc:nongrantee")

	dir := mockIdentities([]syntax.DID{ownerDID, granteeDID, nonGranteeDID})
	p := newPearForTest(t, dir)

	coll := syntax.NSID("my.fake.collection")
	rkey := syntax.RecordKey("my-rkey")
	validate := true

	_, err := p.PutRecord(t.Context(), ownerDID, ownerDID, coll, map[string]any{"data": "value"}, rkey, &validate, []permissions.Grantee{permissions.DIDGrantee(granteeDID)})
	require.NoError(t, err)

	t.Run("owner can check if owner has permission", func(t *testing.T) {
		ok, err := p.HasPermission(t.Context(), ownerDID, ownerDID, ownerDID, coll, rkey)
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("owner can check if grantee has permission", func(t *testing.T) {
		ok, err := p.HasPermission(t.Context(), ownerDID, granteeDID, ownerDID, coll, rkey)
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("owner can check if non-grantee has permission, gets false with no error", func(t *testing.T) {
		ok, err := p.HasPermission(t.Context(), ownerDID, nonGranteeDID, ownerDID, coll, rkey)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("grantee can check their own permission", func(t *testing.T) {
		ok, err := p.HasPermission(t.Context(), granteeDID, granteeDID, ownerDID, coll, rkey)
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("grantee can check if non-grantee has permission, gets false with no error", func(t *testing.T) {
		ok, err := p.HasPermission(t.Context(), granteeDID, nonGranteeDID, ownerDID, coll, rkey)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("non-grantee caller is unauthorized to check any permissions", func(t *testing.T) {
		_, err := p.HasPermission(t.Context(), nonGranteeDID, granteeDID, ownerDID, coll, rkey)
		require.ErrorIs(t, err, ErrUnauthorized)
	})

	t.Run("non-grantee cannot check even the owner's permission on a record", func(t *testing.T) {
		_, err := p.HasPermission(t.Context(), nonGranteeDID, ownerDID, ownerDID, coll, rkey)
		require.ErrorIs(t, err, ErrUnauthorized)
	})
}

// TestAddPermissions tests the AddPermissions pear method's authz logic.
// The rule: only the record owner can grant permissions on their records.
func TestAddPermissions(t *testing.T) {
	ownerDID := syntax.DID("did:plc:owner")
	granteeDID := syntax.DID("did:plc:grantee")
	nonOwnerDID := syntax.DID("did:plc:nonowner")

	dir := mockIdentities([]syntax.DID{ownerDID, granteeDID, nonOwnerDID})
	p := newPearForTest(t, dir)

	coll := syntax.NSID("my.fake.collection")
	rkey := syntax.RecordKey("my-rkey")
	validate := true

	_, err := p.PutRecord(t.Context(), ownerDID, ownerDID, coll, map[string]any{"data": "value"}, rkey, &validate, []permissions.Grantee{})
	require.NoError(t, err)

	t.Run("non-owner cannot add permissions", func(t *testing.T) {
		_, err := p.AddPermissions(t.Context(), nonOwnerDID, []permissions.Grantee{permissions.DIDGrantee(nonOwnerDID)}, ownerDID, coll, rkey)
		require.ErrorIs(t, err, ErrUnauthorized)

		// The non-owner did not gain access as a result of the failed call
		got, err := p.GetRecord(t.Context(), coll, rkey, ownerDID, nonOwnerDID)
		require.Nil(t, got)
		require.ErrorIs(t, err, ErrUnauthorized)
	})

	t.Run("owner can add permissions for a grantee", func(t *testing.T) {
		_, err := p.AddPermissions(t.Context(), ownerDID, []permissions.Grantee{permissions.DIDGrantee(granteeDID)}, ownerDID, coll, rkey)
		require.NoError(t, err)

		// Grantee can now access the record
		got, err := p.GetRecord(t.Context(), coll, rkey, ownerDID, granteeDID)
		require.NoError(t, err)
		require.NotNil(t, got)
	})

	t.Run("owner can add permissions for multiple grantees at once", func(t *testing.T) {
		coll2 := syntax.NSID("my.second.collection")
		rkey2 := syntax.RecordKey("rkey2")
		_, err := p.PutRecord(t.Context(), ownerDID, ownerDID, coll2, map[string]any{"k": "v"}, rkey2, &validate, []permissions.Grantee{})
		require.NoError(t, err)

		_, err = p.AddPermissions(t.Context(), ownerDID, []permissions.Grantee{
			permissions.DIDGrantee(granteeDID),
			permissions.DIDGrantee(nonOwnerDID),
		}, ownerDID, coll2, rkey2)
		require.NoError(t, err)

		for _, did := range []syntax.DID{granteeDID, nonOwnerDID} {
			got, err := p.GetRecord(t.Context(), coll2, rkey2, ownerDID, did)
			require.NoError(t, err, "DID %s should have access", did)
			require.NotNil(t, got)
		}
	})
}

// TestRemovePermissions tests the RemovePermissions pear method's authz logic.
// The rule: only the record owner can revoke permissions on their records.
func TestRemovePermissions(t *testing.T) {
	ownerDID := syntax.DID("did:plc:owner")
	granteeDID := syntax.DID("did:plc:grantee")
	nonOwnerDID := syntax.DID("did:plc:nonowner")

	dir := mockIdentities([]syntax.DID{ownerDID, granteeDID, nonOwnerDID})
	p := newPearForTest(t, dir)

	coll := syntax.NSID("my.fake.collection")
	rkey := syntax.RecordKey("my-rkey")
	validate := true

	_, err := p.PutRecord(t.Context(), ownerDID, ownerDID, coll, map[string]any{"data": "value"}, rkey, &validate, []permissions.Grantee{permissions.DIDGrantee(granteeDID)})
	require.NoError(t, err)

	t.Run("non-owner cannot remove permissions", func(t *testing.T) {
		err := p.RemovePermissions(nonOwnerDID, []permissions.Grantee{permissions.DIDGrantee(granteeDID)}, ownerDID, coll, rkey)
		require.ErrorIs(t, err, ErrUnauthorized)

		// Grantee's access is unaffected after the failed remove
		got, err := p.GetRecord(t.Context(), coll, rkey, ownerDID, granteeDID)
		require.NoError(t, err)
		require.NotNil(t, got)
	})

	t.Run("owner can remove permissions, revoking grantee access", func(t *testing.T) {
		err := p.RemovePermissions(ownerDID, []permissions.Grantee{permissions.DIDGrantee(granteeDID)}, ownerDID, coll, rkey)
		require.NoError(t, err)

		// Grantee no longer has access
		got, err := p.GetRecord(t.Context(), coll, rkey, ownerDID, granteeDID)
		require.Nil(t, got)
		require.ErrorIs(t, err, ErrUnauthorized)
	})

	t.Run("owner can remove collection-level permission, revoking access to all records in that collection", func(t *testing.T) {
		coll2 := syntax.NSID("my.second.collection")
		rkey2a := syntax.RecordKey("rkey2a")
		rkey2b := syntax.RecordKey("rkey2b")
		_, err := p.PutRecord(t.Context(), ownerDID, ownerDID, coll2, map[string]any{"k": "v"}, rkey2a, &validate, []permissions.Grantee{})
		require.NoError(t, err)
		_, err = p.PutRecord(t.Context(), ownerDID, ownerDID, coll2, map[string]any{"k": "v"}, rkey2b, &validate, []permissions.Grantee{})
		require.NoError(t, err)

		// Grant collection-level access (empty rkey)
		_, err = p.AddPermissions(t.Context(), ownerDID, []permissions.Grantee{permissions.DIDGrantee(granteeDID)}, ownerDID, coll2, "")
		require.NoError(t, err)

		// Confirm access before revocation
		got, err := p.GetRecord(t.Context(), coll2, rkey2a, ownerDID, granteeDID)
		require.NoError(t, err)
		require.NotNil(t, got)

		// Remove collection-level permission
		require.NoError(t, p.RemovePermissions(ownerDID, []permissions.Grantee{permissions.DIDGrantee(granteeDID)}, ownerDID, coll2, ""))

		// Grantee no longer has access to any record in the collection
		got, err = p.GetRecord(t.Context(), coll2, rkey2a, ownerDID, granteeDID)
		require.Nil(t, got)
		require.ErrorIs(t, err, ErrUnauthorized)

		got, err = p.GetRecord(t.Context(), coll2, rkey2b, ownerDID, granteeDID)
		require.Nil(t, got)
		require.ErrorIs(t, err, ErrUnauthorized)
	})
}

// TestListPermissionGrants tests the ListPermissionGrants pear method's authz logic.
// The rule: only the granter can enumerate their own outgoing grants.
func TestListPermissionGrants(t *testing.T) {
	ownerDID := syntax.DID("did:plc:owner")
	granteeDID := syntax.DID("did:plc:grantee")
	otherDID := syntax.DID("did:plc:other")

	dir := mockIdentities([]syntax.DID{ownerDID, granteeDID, otherDID})
	p := newPearForTest(t, dir)

	coll := syntax.NSID("my.fake.collection")
	rkey := syntax.RecordKey("my-rkey")
	validate := true

	_, err := p.PutRecord(t.Context(), ownerDID, ownerDID, coll, map[string]any{"data": "value"}, rkey, &validate, []permissions.Grantee{permissions.DIDGrantee(granteeDID)})
	require.NoError(t, err)

	t.Run("caller that is not the granter gets ErrUnauthorized", func(t *testing.T) {
		_, err := p.ListPermissionGrants(t.Context(), otherDID, ownerDID)
		require.ErrorIs(t, err, ErrUnauthorized)
	})

	t.Run("caller == granter can list their outgoing grants", func(t *testing.T) {
		grants, err := p.ListPermissionGrants(t.Context(), ownerDID, ownerDID)
		require.NoError(t, err)
		require.NotEmpty(t, grants)

		found := false
		for _, g := range grants {
			if g.Grantee.String() == granteeDID.String() {
				found = true
			}
		}
		require.True(t, found, "expected to find a grant to granteeDID")
	})

	t.Run("all returned grants belong to the granter as owner", func(t *testing.T) {
		grants, err := p.ListPermissionGrants(t.Context(), ownerDID, ownerDID)
		require.NoError(t, err)
		for _, g := range grants {
			require.Equal(t, ownerDID, g.Owner, "all grants should be owned by ownerDID")
		}
	})

	t.Run("granter with no outgoing grants returns empty list", func(t *testing.T) {
		grants, err := p.ListPermissionGrants(t.Context(), granteeDID, granteeDID)
		require.NoError(t, err)
		require.Empty(t, grants)
	})
}
