package spaces_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	spacestest "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// This is the on-disk schema proposed in Habitat PR #947. Keep it independent
// of the implementation so the test exercises an upgrade from that schema.
type upstreamBlobRef struct {
	Cid        string                  `gorm:"primaryKey"`
	Space      habitat_syntax.SpaceURI `gorm:"primaryKey"`
	Repo       syntax.DID              `gorm:"primaryKey"`
	Collection syntax.NSID             `gorm:"primaryKey"`
	Rkey       syntax.RecordKey        `gorm:"primaryKey"`
}

func (upstreamBlobRef) TableName() string { return "blob_refs" }

func TestBlobReferenceUpgradeRequiresOwnedUpload(t *testing.T) {
	testStorageDatabases(t, func(t *testing.T, database *gorm.DB) {
		require.NoError(t, database.AutoMigrate(&upstreamBlobRef{}))
		store := spacestest.NewTestStore(t, spacestest.WithDB(database))
		space, err := store.CreateSpace(t.Context(), orgID, groupType, "upgrade")
		require.NoError(t, err)
		blobs := spacestest.NewTestBlobStore(t)
		c, size, err := blobs.PutBlob(t.Context(), "text/plain", []byte("synthetic upgrade bytes"))
		require.NoError(t, err)
		require.NoError(t, store.RegisterBlobUpload(t.Context(), owner, c))
		value := spacestest.MustMarshalRecord(t, map[string]any{
			"blob": atdata.Blob{Ref: atdata.CIDLink(c), MimeType: "text/plain", Size: size},
		})
		_, _, err = store.PutRecord(t.Context(), space, owner, groupType, "legacy", value)
		require.NoError(t, err)
		ref := upstreamBlobRef{
			Cid:        c.String(),
			Space:      space,
			Repo:       owner,
			Collection: groupType,
			Rkey:       "legacy",
		}
		require.NoError(t, database.Clauses(clause.OnConflict{DoNothing: true}).Create(&ref).Error)
		// A PR #947 database has records and reference rows, but no authenticated
		// upload history. Neither fact proves that this repo supplied the bytes.
		require.NoError(t, database.Exec("DELETE FROM blob_uploads").Error)
		store = spacestest.NewTestStore(t, spacestest.WithDB(database))
		referenced, err := store.BlobReferenced(t.Context(), space, c)
		require.NoError(t, err)
		require.False(t, referenced, "legacy references must not imply upload ownership")
		require.NoError(t, store.RegisterBlobUpload(t.Context(), alice, c))
		referenced, err = store.BlobReferenced(t.Context(), space, c)
		require.NoError(t, err)
		require.False(
			t,
			referenced,
			"an unrelated repo's upload must not authorize a legacy reference",
		)
		require.NoError(t, store.RegisterBlobUpload(t.Context(), owner, c))
		store = spacestest.NewTestStore(t, spacestest.WithDB(database))
		referenced, err = store.BlobReferenced(t.Context(), space, c)
		require.NoError(t, err)
		require.True(t, referenced, "reuse the upstream reference after an authenticated re-upload")
		require.False(t, database.Migrator().HasTable("space_blob_refs"), "use one reference table")
	})
}

func TestBlobReferenceUpgradeIgnoresDeletedAndMissingRecords(t *testing.T) {
	testStorageDatabases(t, func(t *testing.T, database *gorm.DB) {
		require.NoError(t, database.AutoMigrate(&upstreamBlobRef{}))
		store := spacestest.NewTestStore(t, spacestest.WithDB(database))
		space, err := store.CreateSpace(t.Context(), orgID, groupType, "stale")
		require.NoError(t, err)
		blobs := spacestest.NewTestBlobStore(t)
		c, size, err := blobs.PutBlob(t.Context(), "text/plain", []byte("synthetic stale bytes"))
		require.NoError(t, err)
		require.NoError(t, store.RegisterBlobUpload(t.Context(), owner, c))
		value := spacestest.MustMarshalRecord(t, map[string]any{
			"blob": atdata.Blob{Ref: atdata.CIDLink(c), MimeType: "text/plain", Size: size},
		})
		_, _, err = store.PutRecord(t.Context(), space, owner, groupType, "deleted", value)
		require.NoError(t, err)
		require.NoError(t, store.DeleteRecord(t.Context(), space, owner, groupType, "deleted"))
		// Model stale rows left by an older deployment or an interrupted repair.
		for _, key := range []syntax.RecordKey{"deleted", "missing"} {
			ref := upstreamBlobRef{
				Cid:        c.String(),
				Space:      space,
				Repo:       owner,
				Collection: groupType,
				Rkey:       key,
			}
			require.NoError(
				t,
				database.Clauses(clause.OnConflict{DoNothing: true}).Create(&ref).Error,
			)
		}
		store = spacestest.NewTestStore(t, spacestest.WithDB(database))
		referenced, err := store.BlobReferenced(t.Context(), space, c)
		require.NoError(t, err)
		require.False(t, referenced, "only a live record can authorize a blob")
	})
}
