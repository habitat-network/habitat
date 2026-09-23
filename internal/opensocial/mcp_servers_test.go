package opensocial_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
)

func TestMcpServers(t *testing.T) {
	s := opensocial_testutil.NewTestStore(t)
	creator := syntax.DID("did:plc:creator")
	orgDIDStr, err := s.NewOrg(t.Context(), "acme", creator)
	require.NoError(t, err)
	org := syntax.DID(orgDIDStr)

	t.Run("PutAndListMcpServers", func(t *testing.T) {
		id := syntax.RecordKey(uuid.NewString())
		server, err := s.PutMcpServer(t.Context(), org, id, "Linear", "desc", "nango-key-1")
		require.NoError(t, err)
		require.Equal(t, id, server.ID)
		require.Equal(t, "Linear", server.Name)
		require.Equal(t, "desc", server.Description)
		require.Equal(t, "nango-key-1", server.NangoKey)

		servers, err := s.ListMcpServers(t.Context(), org)
		require.NoError(t, err)
		require.Len(t, servers, 1)
		require.Equal(t, server.ID, servers[0].ID)
	})

	t.Run("GetMcpServer_NotFound", func(t *testing.T) {
		_, err := s.GetMcpServer(t.Context(), org, "nonexistent")
		require.ErrorIs(t, err, opensocial.ErrMcpServerNotFound)
	})

	t.Run("UpdateMcpServer", func(t *testing.T) {
		id := syntax.RecordKey(uuid.NewString())
		_, err := s.PutMcpServer(t.Context(), org, id, "Open", "", "nango-key-2")
		require.NoError(t, err)

		newDescription := "now with a description"
		updated, err := s.UpdateMcpServer(t.Context(), org, id, &newDescription)
		require.NoError(t, err)
		require.Equal(t, "Open", updated.Name)
		require.Equal(t, "now with a description", updated.Description)
		require.Equal(t, "nango-key-2", updated.NangoKey) // unchanged

		fetched, err := s.GetMcpServer(t.Context(), org, id)
		require.NoError(t, err)
		require.Equal(t, "now with a description", fetched.Description)
	})

	t.Run("RemoveMcpServer", func(t *testing.T) {
		id := syntax.RecordKey(uuid.NewString())
		_, err := s.PutMcpServer(t.Context(), org, id, "Temp", "", "nango-key-3")
		require.NoError(t, err)

		require.NoError(t, s.RemoveMcpServer(t.Context(), org, id))

		_, err = s.GetMcpServer(t.Context(), org, id)
		require.ErrorIs(t, err, opensocial.ErrMcpServerNotFound)

		err = s.RemoveMcpServer(t.Context(), org, id)
		require.ErrorIs(t, err, opensocial.ErrMcpServerNotFound)
	})

	t.Run("ScopedPerOrg", func(t *testing.T) {
		otherOrgDIDStr, err := s.NewOrg(t.Context(), "other", creator)
		require.NoError(t, err)
		otherOrg := syntax.DID(otherOrgDIDStr)

		id := syntax.RecordKey(uuid.NewString())
		_, err = s.PutMcpServer(t.Context(), org, id, "Scoped", "", "nango-key-4")
		require.NoError(t, err)

		_, err = s.GetMcpServer(t.Context(), otherOrg, id)
		require.ErrorIs(t, err, opensocial.ErrMcpServerNotFound)
	})
}
