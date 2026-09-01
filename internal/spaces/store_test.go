package spaces_test

import (
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"

	notify_testutil "github.com/habitat-network/habitat/internal/notify/testutil"
	"github.com/habitat-network/habitat/internal/spacecommit"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	"github.com/stretchr/testify/require"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

var (
	orgID     = syntax.DID("did:plc:org")
	owner     = syntax.DID("did:plc:owner")
	alice     = syntax.DID("did:plc:alice")
	groupType = syntax.NSID("network.habitat.group")
)

func TestCreateSpace(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "my-group")
	require.NoError(t, err)
	require.Equal(t, "at://did:plc:org/space/network.habitat.group/my-group", uri.String())
}

func TestCreateSpace_AutoSkey(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "")
	require.NoError(t, err)
	require.Contains(t, uri, "at://did:plc:org/space/network.habitat.group/")
}

func TestCreateSpace_Duplicate(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	_, err := s.CreateSpace(t.Context(), orgID, groupType, "dup")
	require.NoError(t, err)

	_, err = s.CreateSpace(t.Context(), orgID, groupType, "dup")
	require.ErrorIs(t, err, spaces.ErrSpaceAlreadyExists)
}

func TestListSpaces(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	space1, err := s.CreateSpace(t.Context(), orgID, groupType, "space1")
	require.NoError(t, err)

	space2, err := s.CreateSpace(t.Context(), orgID, groupType, "space2")
	require.NoError(t, err)

	// Owning or being granted access to a space alone doesn't put it on a
	// member's listing: listSpaces reports the spaces the caller holds a
	// permissioned repo in — the ones it has written to — and consults no
	// access store.
	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.PutRecord(t.Context(), space1, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)
	_, _, err = s.PutRecord(t.Context(), space2, alice, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)

	// Owner sees the space it wrote to, not the one it was merely granted read
	// access to.
	spaces, err := s.ListSpaces(t.Context(), owner, nil, nil)
	require.NoError(t, err)
	require.Len(t, spaces, 1)
	require.Equal(t, space1, spaces[0])

	// Alice sees the space she wrote to.
	spaces, err = s.ListSpaces(t.Context(), alice, nil, nil)
	require.NoError(t, err)
	require.Len(t, spaces, 1)
	require.Equal(t, space2, spaces[0])
}

func TestListSpaces_OwningASpaceIsNotWritingToIt(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	space1, err := s.CreateSpace(t.Context(), orgID, groupType, "space1")
	require.NoError(t, err)
	space2, err := s.CreateSpace(t.Context(), orgID, groupType, "space2")
	require.NoError(t, err)

	// Owning a space is not writing to it: the org holds no repo in either
	// space yet, so it lists neither.
	spaces, err := s.ListSpaces(t.Context(), orgID, nil, nil)
	require.NoError(t, err)
	require.Empty(t, spaces)

	// A write on the org's behalf — how relationship tuples land in a space —
	// puts that one space, and only that one, on its listing.
	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.PutRecord(t.Context(), space1, orgID, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)

	spaces, err = s.ListSpaces(t.Context(), orgID, nil, nil)
	require.NoError(t, err)
	require.Equal(t, []habitat_syntax.SpaceURI{space1}, spaces)
	require.NotContains(t, spaces, space2)
}

func TestListSpaces_LeavesListingAfterSpaceIsDeleted(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "space1")
	require.NoError(t, err)
	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)

	// Deleting the space drops its permissioned repos, so it leaves the
	// listings of everyone who had written to it.
	require.NoError(t, s.DeleteSpace(t.Context(), uri))

	spaces, err := s.ListSpaces(t.Context(), owner, nil, nil)
	require.NoError(t, err)
	require.Empty(t, spaces)
}

func TestListSpaces_LeavesListingAfterDeletingAllRecords(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)

	spaces, err := s.ListSpaces(t.Context(), owner, nil, nil)
	require.NoError(t, err)
	require.Len(t, spaces, 1)

	// Deleting the only record removes the repo from the writer set, so the
	// space drops out of the caller's listing.
	require.NoError(t, s.DeleteRecord(t.Context(), uri, owner, coll, "k1"))
	spaces, err = s.ListSpaces(t.Context(), owner, nil, nil)
	require.NoError(t, err)
	require.Len(t, spaces, 0)
}

func TestListSpaces_FilterByType(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	personal := syntax.NSID("network.habitat.personal")

	group1, err := s.CreateSpace(t.Context(), orgID, groupType, "group1")
	require.NoError(t, err)

	personal1, err := s.CreateSpace(t.Context(), orgID, personal, "personal1")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.PutRecord(t.Context(), group1, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)
	_, _, err = s.PutRecord(t.Context(), personal1, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)

	spaces, err := s.ListSpaces(t.Context(), owner, nil, &groupType)
	require.NoError(t, err)
	require.Equal(t, []habitat_syntax.SpaceURI{group1}, spaces)
}

// The owner filter matches the DID inside the stored URI, so a DID carrying a
// LIKE wildcard — a did:web percent-encodes the port it holds — must not match
// any other owner.
func TestListSpaces_FilterByOwnerWithWildcardInDID(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	orgWithPort := syntax.DID("did:web:example.com%3A8080")
	// Differs from orgWithPort only where the % sits, so it matches if the %
	// is left to act as a wildcard.
	otherOrg := syntax.DID("did:web:example.comZZ3A8080")

	wanted, err := s.CreateSpace(t.Context(), orgWithPort, groupType, "space1")
	require.NoError(t, err)
	other, err := s.CreateSpace(t.Context(), otherOrg, groupType, "space2")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.PutRecord(t.Context(), wanted, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)
	_, _, err = s.PutRecord(t.Context(), other, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)

	spaces, err := s.ListSpaces(t.Context(), owner, &orgWithPort, nil)
	require.NoError(t, err)
	require.Equal(t, []habitat_syntax.SpaceURI{wanted}, spaces)
}

func TestListSpaces_NilOwnerFilterSpansAllOrgs(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	orgA := syntax.DID("did:plc:org-a")
	orgB := syntax.DID("did:plc:org-b")
	member := syntax.DID("did:plc:cross-org-member")

	spaceA, err := s.CreateSpace(t.Context(), orgA, groupType, "space-a")
	require.NoError(t, err)
	spaceB, err := s.CreateSpace(t.Context(), orgB, groupType, "space-b")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.PutRecord(t.Context(), spaceA, member, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)
	_, _, err = s.PutRecord(t.Context(), spaceB, member, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)

	// With no owner filter, the member sees spaces across every org they
	// belong to, not just one.
	spaces, err := s.ListSpaces(t.Context(), member, nil, nil)
	require.NoError(t, err)
	require.ElementsMatch(t, []habitat_syntax.SpaceURI{spaceA, spaceB}, spaces)

	// Filtering by a specific org owner restricts the results to that org.
	spaces, err = s.ListSpaces(t.Context(), member, &orgA, nil)
	require.NoError(t, err)
	require.Len(t, spaces, 1)
	require.Equal(t, spaceA, spaces[0])
}

func TestListRepos_Empty(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	repos, err := s.ListRepos(t.Context(), uri)
	require.NoError(t, err)
	require.Empty(t, repos)
}

func TestListRepos_WithRecords(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)
	_, _, err = s.PutRecord(t.Context(), uri, alice, coll, "k2", map[string]any{"x": 2})
	require.NoError(t, err)

	repos, err := s.ListRepos(t.Context(), uri)
	require.NoError(t, err)
	require.Len(t, repos, 2)

	dids := make([]syntax.DID, len(repos))
	for i, r := range repos {
		dids[i] = r.DID
	}
	require.ElementsMatch(t, []syntax.DID{owner, alice}, dids)
	require.NotEmpty(t, repos[0].Rev)
}

// TestListRepos_HashMatchesLtHash verifies the reported repo hash equals an
// independently computed LtHash over the repo's (collection/rkey/cid) elements.
func TestListRepos_HashMatchesLtHash(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, cid1, err := s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)
	_, cid2, err := s.PutRecord(t.Context(), uri, owner, coll, "k2", map[string]any{"x": 2})
	require.NoError(t, err)

	repos, err := s.ListRepos(t.Context(), uri)
	require.NoError(t, err)
	require.Len(t, repos, 1)

	var expected spacecommit.LtHash
	expected.Add(spacecommit.RecordElement(coll, "k1", cid1.String()))
	expected.Add(spacecommit.RecordElement(coll, "k2", cid2.String()))
	require.Equal(t, expected.Sum(), repos[0].Hash)
}

func TestListRepos_SpaceNotFound(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")
	_, err := s.ListRepos(t.Context(), uri)
	require.ErrorIs(t, err, spaces.ErrSpaceNotFound)
}

func TestPutAndGetRecord(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	val := map[string]any{"text": "hello world"}

	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "my-rkey", val)
	require.NoError(t, err)

	rec, err := s.GetRecord(t.Context(), uri, owner, coll, "my-rkey")
	require.NoError(t, err)
	require.Equal(t, val, rec.Value)
	require.Equal(t, syntax.RecordKey("my-rkey"), rec.Rkey)
}

// TestPutRecord_SpaceNotFound pins that PutRecord refuses to write into a
// space that was never created, rather than silently creating an orphaned
// record row.
func TestPutRecord_SpaceNotFound(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")
	coll := syntax.NSID("network.habitat.note")
	_, _, err := s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.ErrorIs(t, err, spaces.ErrSpaceNotFound)
}

func TestPutRecord_UpdateExisting(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")

	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "rkey", map[string]any{"v": 1})
	require.NoError(t, err)

	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "rkey", map[string]any{"v": 2})
	require.NoError(t, err)

	rec, err := s.GetRecord(t.Context(), uri, owner, coll, "rkey")
	require.NoError(t, err)
	require.Equal(t, int64(2), rec.Value["v"])
}

// blobValue returns a record value with a single atproto blob field
// referencing cidStr, in the raw JSON shape ExtractBlobs recognizes.
func blobValue(cidStr string) map[string]any {
	return map[string]any{
		"$type": "network.habitat.note",
		"image": map[string]any{
			"$type":    "blob",
			"ref":      map[string]any{"$link": cidStr},
			"mimeType": "image/png",
			"size":     float64(42),
		},
	}
}

func TestPutRecord_TracksBlobReferences(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	blobCID := "bafkreihdwdcefgh4dqkjv67uzcmw37nwqfknnagwmjb44agkh4lphqzgxq"

	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "rkey", blobValue(blobCID))
	require.NoError(t, err)

	c, err := cid.Decode(blobCID)
	require.NoError(t, err)
	referenced, err := s.BlobReferenced(t.Context(), uri, c)
	require.NoError(t, err)
	require.True(t, referenced)
}

// TestPutRecord_ClearsStaleBlobReferences pins that overwriting a record
// drops its prior blob references rather than accumulating them: a blob
// referenced by an old value must not stay authorized once the record no
// longer references it.
func TestPutRecord_ClearsStaleBlobReferences(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	blobCID := "bafkreihdwdcefgh4dqkjv67uzcmw37nwqfknnagwmjb44agkh4lphqzgxq"

	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "rkey", blobValue(blobCID))
	require.NoError(t, err)

	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "rkey", map[string]any{"text": "no blob"})
	require.NoError(t, err)

	c, err := cid.Decode(blobCID)
	require.NoError(t, err)
	referenced, err := s.BlobReferenced(t.Context(), uri, c)
	require.NoError(t, err)
	require.False(t, referenced)
}

// TestDeleteRecord_ClearsBlobReferences pins that deleting the only record
// referencing a blob revokes read access to that blob within the space.
func TestDeleteRecord_ClearsBlobReferences(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	blobCID := "bafkreihdwdcefgh4dqkjv67uzcmw37nwqfknnagwmjb44agkh4lphqzgxq"

	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "rkey", blobValue(blobCID))
	require.NoError(t, err)

	require.NoError(t, s.DeleteRecord(t.Context(), uri, owner, coll, "rkey"))

	c, err := cid.Decode(blobCID)
	require.NoError(t, err)
	referenced, err := s.BlobReferenced(t.Context(), uri, c)
	require.NoError(t, err)
	require.False(t, referenced)
}

func TestBlobReferenced_UnreferencedCidIsFalse(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	c, err := cid.Decode("bafkreihdwdcefgh4dqkjv67uzcmw37nwqfknnagwmjb44agkh4lphqzgxq")
	require.NoError(t, err)
	referenced, err := s.BlobReferenced(t.Context(), uri, c)
	require.NoError(t, err)
	require.False(t, referenced)
}

func TestGetRecord_NotFound(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, err = s.GetRecord(t.Context(), uri, owner, coll, "nonexistent")
	require.ErrorIs(t, err, spaces.ErrRecordNotFound)
}

func TestListRecords(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	collA := syntax.NSID("network.habitat.alpha")
	collB := syntax.NSID("network.habitat.beta")
	_, _, err = s.PutRecord(t.Context(), uri, owner, collA, "k1", map[string]any{"x": 1})
	require.NoError(t, err)
	_, _, err = s.PutRecord(t.Context(), uri, owner, collA, "k2", map[string]any{"x": 2})
	require.NoError(t, err)
	_, _, err = s.PutRecord(t.Context(), uri, owner, collB, "k1", map[string]any{"x": 3})
	require.NoError(t, err)

	// All records
	records, err := s.ListRecords(t.Context(), uri, owner, nil)
	require.NoError(t, err)
	require.Len(t, records, 3)

	// Filter by collection
	records, err = s.ListRecords(t.Context(), uri, owner, &collA)
	require.NoError(t, err)
	require.Len(t, records, 2)
	for _, r := range records {
		require.Equal(t, collA, r.Collection)
	}
}

func TestPutRecord_RejectsNonIntegerFloat(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 0.15})
	require.ErrorIs(t, err, spaces.ErrInvalidRecord)

	_, err = s.GetRecord(t.Context(), uri, owner, coll, "k1")
	require.ErrorIs(t, err, spaces.ErrRecordNotFound)
}

func TestDeleteRecord(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "rkey", map[string]any{"x": 1})
	require.NoError(t, err)

	err = s.DeleteRecord(t.Context(), uri, owner, coll, "rkey")
	require.NoError(t, err)

	_, err = s.GetRecord(t.Context(), uri, owner, coll, "rkey")
	require.ErrorIs(t, err, spaces.ErrRecordNotFound)

	ops, _, err := s.ListRepoOps(t.Context(), uri, owner, "", 100)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	require.Empty(t, ops[0].Cid)
}

// TestRepoHash_IncrementalOnUpdateAndDelete verifies the cached LtHash is
// maintained in the write path: an update folds the old cid out (not just the
// new one in), and deleting the last record drops the repo from the writer set.
func TestRepoHash_IncrementalOnUpdateAndDelete(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)
	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)
	coll := syntax.NSID("network.habitat.note")

	_, cid1, err := s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"v": 1})
	require.NoError(t, err)
	var want1 spacecommit.LtHash
	want1.Add(spacecommit.RecordElement(coll, "k1", cid1.String()))
	_, hash, found, err := s.RepoHead(t.Context(), uri, owner)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, want1.Sum(), hash)

	// Update the same rkey: the old element must be folded out and the new in.
	_, cid2, err := s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"v": 2})
	require.NoError(t, err)
	require.NotEqual(t, cid1.String(), cid2.String())
	var want2 spacecommit.LtHash
	want2.Add(spacecommit.RecordElement(coll, "k1", cid2.String()))
	_, hash, _, err = s.RepoHead(t.Context(), uri, owner)
	require.NoError(t, err)
	require.Equal(t, want2.Sum(), hash, "update must not leave the old cid folded in")

	// Delete the last record: the repo leaves the writer set.
	require.NoError(t, s.DeleteRecord(t.Context(), uri, owner, coll, "k1"))
	_, _, found, err = s.RepoHead(t.Context(), uri, owner)
	require.NoError(t, err)
	require.False(t, found)

	repos, err := s.ListRepos(t.Context(), uri)
	require.NoError(t, err)
	require.Empty(t, repos)
}

func TestDeleteRecord_Nonexistent(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	// Deleting a nonexistent record should not error
	err = s.DeleteRecord(
		t.Context(),
		uri,
		owner,
		syntax.NSID("network.habitat.note"),
		"nonexistent",
	)
	require.NoError(t, err)
}

func TestSpaceURI(t *testing.T) {
	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "my-key")
	require.Equal(t, "at://did:plc:owner/space/network.habitat.group/my-key", uri.String())
	require.Equal(t, owner, uri.SpaceOwner())
	require.Equal(t, groupType, uri.SpaceType())
	require.Equal(t, habitat_syntax.SpaceKey("my-key"), uri.Skey())

	parsed, err := habitat_syntax.ParseSpaceURI(
		"at://did:plc:owner/space/network.habitat.group/my-key",
	)
	require.NoError(t, err)
	require.Equal(t, uri, parsed)
}

func TestParseSpaceURI_Invalid(t *testing.T) {
	tests := []struct {
		input string
		name  string
	}{
		{"", "empty"},
		{"at://notadid/space/network.habitat.group/key", "invalid did"},
		{"notaspace", "no scheme"},
		{"at://did:plc:abc", "missing type and key"},
		{"at://did:plc:abc/space/notansid/key", "invalid type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := habitat_syntax.ParseSpaceURI(tt.input)
			require.Error(t, err)
		})
	}
}

func TestDeleteSpace(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "to-delete")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "r1", map[string]any{"x": 1})
	require.NoError(t, err)
	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "r2", map[string]any{"x": 2})
	require.NoError(t, err)

	err = s.DeleteSpace(t.Context(), uri)
	require.NoError(t, err)

	// space should be gone
	_, err = s.ListRepos(t.Context(), uri)
	require.ErrorIs(t, err, spaces.ErrSpaceNotFound)

	// records should be gone
	records, err := s.ListRecords(t.Context(), uri, owner, nil)
	require.NoError(t, err)
	require.Len(t, records, 0)
}

func TestDeleteSpace_NonExistent(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)
	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")
	err := s.DeleteSpace(t.Context(), uri)
	require.ErrorIs(t, err, spaces.ErrSpaceNotFound)
}

func TestListRepoOps(t *testing.T) {
	ctx := t.Context()
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(ctx, orgID, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")

	t.Run("empty", func(t *testing.T) {
		records, _, err := s.ListRepoOps(ctx, uri, "did:web:unknown", "", 100)
		require.NoError(t, err)
		require.Len(t, records, 0)
	})
	t.Run("multiple", func(t *testing.T) {
		_, _, err = s.PutRecord(ctx, uri, owner, coll, "k1", map[string]any{"x": 1})
		require.NoError(t, err)
		_, _, err = s.PutRecord(ctx, uri, alice, coll, "k2", map[string]any{"x": 2})
		require.NoError(t, err)
		_, _, err = s.PutRecord(ctx, uri, owner, coll, "k3", map[string]any{"x": 3})
		require.NoError(t, err)

		records, _, err := s.ListRepoOps(ctx, uri, owner, "", 1)
		require.NoError(t, err)
		require.Len(t, records, 1)
		require.Equal(t, syntax.RecordKey("k1"), records[0].Rkey)

		records, _, err = s.ListRepoOps(ctx, uri, owner, records[0].Rev, 100)
		require.NoError(t, err)
		require.Len(t, records, 1)
		require.Equal(t, syntax.RecordKey("k3"), records[0].Rkey)

		ownerLastRev := records[0].Rev

		records, _, err = s.ListRepoOps(ctx, uri, alice, "", 100)
		require.NoError(t, err)
		require.Len(t, records, 1)
		require.Equal(t, syntax.RecordKey("k2"), records[0].Rkey)

		aliceLastRev := records[0].Rev

		require.NoError(t, s.DeleteRecord(ctx, uri, owner, coll, "k1"))
		require.NoError(t, s.DeleteRecord(ctx, uri, alice, coll, "k2"))

		records, _, err = s.ListRepoOps(ctx, uri, owner, ownerLastRev, 100)
		require.NoError(t, err)
		require.Len(t, records, 1)
		require.Equal(t, syntax.RecordKey("k1"), records[0].Rkey)
		require.Empty(t, records[0].Cid)

		records, _, err = s.ListRepoOps(ctx, uri, alice, aliceLastRev, 100)
		require.NoError(t, err)
		require.Len(t, records, 1)
		require.Equal(t, syntax.RecordKey("k2"), records[0].Rkey)
		require.Empty(t, records[0].Cid)
	})

	t.Run("includes value", func(t *testing.T) {
		owner := syntax.DID("did:web:bob")
		_, _, err = s.PutRecord(ctx, uri, owner, coll, "k1", map[string]any{"text": "hello"})
		require.NoError(t, err)

		records, _, err := s.ListRepoOps(t.Context(), uri, owner, "", 100)
		require.NoError(t, err)
		require.Len(t, records, 1)
		require.Equal(t, "hello", records[0].Value["text"])
		require.NotEmpty(t, records[0].Rev)
	})
}

func TestPutRecordTriggersNotify(t *testing.T) {
	notifier := &notify_testutil.TestNotifier{}
	s := spaces_testutil.NewTestStore(t, spaces_testutil.WithNotifier(notifier))

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "notify-space")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)

	require.Len(t, notifier.Writes, 1)
	require.Equal(t, uri, notifier.Writes[0].Space)
	require.Equal(t, owner, notifier.Writes[0].Repo)
	require.NotEmpty(t, notifier.Writes[0].Rev)
}

func TestPutRecordSkipsNotifyWhenCidUnchanged(t *testing.T) {
	notifier := &notify_testutil.TestNotifier{}
	s := spaces_testutil.NewTestStore(t, spaces_testutil.WithNotifier(notifier))

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "notify-space")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)
	require.Len(t, notifier.Writes, 1)

	// Writing the exact same value again produces the same CID.
	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)
	require.Len(t, notifier.Writes, 1, "no-op write should not trigger another notify")

	// A write that actually changes the content still notifies.
	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 2})
	require.NoError(t, err)
	require.Len(t, notifier.Writes, 2)
}

func TestDeleteSpaceTriggersNotify(t *testing.T) {
	notifier := &notify_testutil.TestNotifier{}
	s := spaces_testutil.NewTestStore(t, spaces_testutil.WithNotifier(notifier))

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "doomed")
	require.NoError(t, err)

	require.NoError(t, s.DeleteSpace(t.Context(), uri))
	require.Equal(t, []habitat_syntax.SpaceURI{uri}, notifier.Deleted)
}

// TestListRepoOpsPrev tracks the previous cid of each op so a syncer can fold
// the prior element out of its LtHash on updates and deletes.
func TestListRepoOpsPrev(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, cid1, err := s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"v": 1})
	require.NoError(t, err)
	_, cid2, err := s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"v": 2})
	require.NoError(t, err)

	// An update overwrites in place: one op whose prev is the old cid.
	ops, _, err := s.ListRepoOps(t.Context(), uri, owner, "", 100)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	require.Equal(t, cid1.String(), ops[0].Prev)
	require.Equal(t, cid2.String(), ops[0].Cid.String())
	require.NotNil(t, ops[0].Value)

	// A delete soft-removes the row: one op whose prev is the last cid, with no
	// cid and no value.
	require.NoError(t, s.DeleteRecord(t.Context(), uri, owner, coll, "k1"))
	ops, _, err = s.ListRepoOps(t.Context(), uri, owner, "", 100)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	require.Equal(t, cid2.String(), ops[0].Prev)
	require.Empty(t, ops[0].Cid)
	require.Nil(t, ops[0].Value)
}

// TestListRepoOpsPrevCreateIsEmpty pins that a create op has no prev.
func TestListRepoOpsPrevCreateIsEmpty(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"v": 1})
	require.NoError(t, err)

	ops, _, err := s.ListRepoOps(t.Context(), uri, owner, "", 100)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	require.Empty(t, ops[0].Prev)
}

// TestPutRecordNotifiesRepoHash pins that the notifier receives the repo's
// LtHash state so syncers can detect writes that arrive with the same rev but
// a different hash (i.e. a write we missed).
func TestPutRecordNotifiesRepoHash(t *testing.T) {
	notifier := &notify_testutil.TestNotifier{}
	s := spaces_testutil.NewTestStore(t, spaces_testutil.WithNotifier(notifier))

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, cid1, err := s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"v": 1})
	require.NoError(t, err)
	_, cid2, err := s.PutRecord(t.Context(), uri, owner, coll, "k2", map[string]any{"v": 2})
	require.NoError(t, err)

	require.Len(t, notifier.Writes, 2)

	var expected spacecommit.LtHash
	expected.Add(spacecommit.RecordElement(coll, "k1", cid1.String()))
	expected.Add(spacecommit.RecordElement(coll, "k2", cid2.String()))
	require.Equal(t, expected.Sum(), notifier.Writes[1].Hash)
}

// TestListRepoOps_RevTooFar pins that a since cursor beyond the repo's actual
// head is rejected rather than treated as an empty page — a caller ahead of
// the host (e.g. after a host rollback) must fall back to a full resync
// instead of silently stalling.
func TestListRepoOps_RevTooFar(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"v": 1})
	require.NoError(t, err)

	// A TID-like string that sorts after any real TID (base32 is a-z + 2-7).
	ahead := strings.Repeat("z", 13)
	_, _, err = s.ListRepoOps(t.Context(), uri, owner, ahead, 100)
	require.ErrorIs(t, err, spaces.ErrRevTooFar)
}

// TestRepoSnapshot_SpaceNotFound pins that RepoSnapshot — the read path
// behind getRepo — reports ErrSpaceNotFound for a space that was never
// created, distinct from an empty repo within an existing space.
func TestRepoSnapshot_SpaceNotFound(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)

	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")
	_, _, err := s.RepoSnapshot(t.Context(), uri, owner)
	require.ErrorIs(t, err, spaces.ErrSpaceNotFound)
}
