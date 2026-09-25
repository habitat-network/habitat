package spaces_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	spacestest "github.com/habitat-network/habitat/internal/spaces/testutil"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBlobReferenceLifecycle(t *testing.T) {
	testStorageDatabases(t, func(t *testing.T, database *gorm.DB) {
		store := spacestest.NewTestStore(t, spacestest.WithDB(database))
		space, err := store.CreateSpace(t.Context(), orgID, groupType, "references")
		require.NoError(t, err)
		other, err := store.CreateSpace(t.Context(), orgID, groupType, "other-references")
		require.NoError(t, err)
		blobs := spacestest.NewTestBlobStore(t)
		first, firstSize, err := blobs.PutBlob(t.Context(), "text/plain", []byte("first blob"))
		require.NoError(t, err)
		second, secondSize, err := blobs.PutBlob(t.Context(), "text/plain", []byte("second blob"))
		require.NoError(t, err)
		for _, c := range []atdata.CIDLink{atdata.CIDLink(first), atdata.CIDLink(second)} {
			require.NoError(t, store.RegisterBlobUpload(t.Context(), owner, c.CID()))
		}
		value := func(c atdata.CIDLink, size int64) map[string]any {
			return map[string]any{
				"blob": atdata.Blob{Ref: c, MimeType: "text/plain", Size: size},
			}
		}
		put := func(target string, key syntax.RecordKey, c atdata.CIDLink, size int64) {
			t.Helper()
			spaceURI := space
			if target == "other" {
				spaceURI = other
			}
			_, _, err := store.PutRecord(t.Context(), spaceURI, owner, groupType, key,
				spacestest.MustMarshalRecord(t, value(c, size)))
			require.NoError(t, err)
		}
		check := func(target string, c atdata.CIDLink, want bool) {
			t.Helper()
			spaceURI := space
			if target == "other" {
				spaceURI = other
			}
			got, err := store.BlobReferenced(t.Context(), spaceURI, c.CID())
			require.NoError(t, err)
			require.Equal(t, want, got)
		}

		check("space", atdata.CIDLink(first), false)
		put("space", "one", atdata.CIDLink(first), firstSize)
		put("space", "two", atdata.CIDLink(first), firstSize)
		put("other", "one", atdata.CIDLink(first), firstSize)
		check("space", atdata.CIDLink(first), true)
		check("other", atdata.CIDLink(first), true)
		check("other", atdata.CIDLink(second), false)

		put("space", "one", atdata.CIDLink(second), secondSize)
		check("space", atdata.CIDLink(first), true) // The second record still references it.
		check("space", atdata.CIDLink(second), true)
		check("other", atdata.CIDLink(second), false)

		require.NoError(t, store.DeleteRecord(t.Context(), space, owner, groupType, "two"))
		check("space", atdata.CIDLink(first), false)
		check("space", atdata.CIDLink(second), true)
		check("other", atdata.CIDLink(first), true)
		require.NoError(t, store.DeleteRecord(t.Context(), space, owner, groupType, "one"))
		check("space", atdata.CIDLink(second), false)
		check("other", atdata.CIDLink(first), true)
	})
}

func TestBlobReferenceDeleteSpaceClearsRefsBeforeRecreation(t *testing.T) {
	testStorageDatabases(t, func(t *testing.T, database *gorm.DB) {
		store := spacestest.NewTestStore(t, spacestest.WithDB(database))
		space, err := store.CreateSpace(t.Context(), orgID, groupType, "recreated")
		require.NoError(t, err)
		blobs := spacestest.NewTestBlobStore(t)
		c, size, err := blobs.PutBlob(t.Context(), "text/plain", []byte("old space blob"))
		require.NoError(t, err)
		require.NoError(t, store.RegisterBlobUpload(t.Context(), owner, c))
		_, _, err = store.PutRecord(t.Context(), space, owner, groupType, "old",
			spacestest.MustMarshalRecord(t, map[string]any{
				"blob": atdata.Blob{Ref: atdata.CIDLink(c), MimeType: "text/plain", Size: size},
			}))
		require.NoError(t, err)
		referenced, err := store.BlobReferenced(t.Context(), space, c)
		require.NoError(t, err)
		require.True(t, referenced)

		require.NoError(t, store.DeleteSpace(t.Context(), space))
		var remaining int64
		require.NoError(
			t,
			database.Table("blob_refs").Where("space = ?", space).Count(&remaining).Error,
		)
		require.Zero(
			t,
			remaining,
			"deletion must remove refs even though record rows are soft deleted",
		)

		// Deleting a Space permits its URI to be created again, but must not
		// restore authorization from its old records or reference rows.
		recreated, err := store.CreateSpace(t.Context(), orgID, groupType, "recreated")
		require.NoError(t, err)
		require.Equal(t, space, recreated)
		referenced, err = store.BlobReferenced(t.Context(), recreated, c)
		require.NoError(t, err)
		require.False(t, referenced)
	})
}
