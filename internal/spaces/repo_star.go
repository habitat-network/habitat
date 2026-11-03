package spaces

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"

	"github.com/habitat-network/habitat/internal/spacecommit"
	"github.com/habitat-network/habitat/internal/star"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// RepoStarType is the STAR profile of a com.atproto.space.getRepo archive.
const RepoStarType = "atproto-space-repo"

type recordBlock struct {
	Collection syntax.NSID
	Rkey       syntax.RecordKey
	Cid        cid.Cid
	Bytes      []byte
}

func recordPath(collection syntax.NSID, rkey syntax.RecordKey) string {
	return collection.String() + "/" + rkey.String()
}

// repoStarMeta builds the archive's metadata: the repo's commit, typed by the
// atproto-space-repo profile. ver, repo, space, rev and data (the commit's set
// hash) are the profile's fields; ikm, sig, mac and habitatSigned carry the
// rest of network.habitat.space.defs#signedCommit, which readers that only
// know the profile ignore.
func repoStarMeta(
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	c spacecommit.SignedCommit,
) map[string]any {
	meta := map[string]any{
		"$type":         RepoStarType,
		"ver":           int64(c.Ver),
		"repo":          repo.String(),
		"space":         space.String(),
		"rev":           c.Rev,
		"data":          atdata.Bytes(c.Hash),
		"habitatSigned": c.HabitatSigned,
	}
	// Empty optional fields are omitted rather than written: atdata encodes a
	// nil Bytes as CBOR null, and decodes an empty byte string back to nil, so
	// neither would survive the reader's canonical-encoding check.
	for key, value := range map[string][]byte{"ikm": c.Ikm, "sig": c.Sig, "mac": c.Mac} {
		if len(value) > 0 {
			meta[key] = atdata.Bytes(value)
		}
	}
	return meta
}

// SerializeRepoStar serializes a permissioned repo to a STAR archive per
// com.atproto.space.getRepo. commit is the repo's signed head commit and
// becomes the archive's metadata; blocks are its current records, written as
// one "{collection}/{rkey}" → DRISL record pair each, in strict bytewise order
// of the path.
func SerializeRepoStar(
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	commit spacecommit.SignedCommit,
	blocks []recordBlock,
) ([]byte, error) {
	sorted := slices.Clone(blocks)
	slices.SortFunc(sorted, func(a, b recordBlock) int {
		return strings.Compare(
			recordPath(a.Collection, a.Rkey),
			recordPath(b.Collection, b.Rkey),
		)
	})

	var buf bytes.Buffer
	w, err := star.NewWriter(&buf, repoStarMeta(space, repo, commit))
	if err != nil {
		return nil, err
	}
	for i, block := range sorted {
		path := recordPath(block.Collection, block.Rkey)
		// The profile requires strictly increasing paths.
		if i > 0 && path == recordPath(sorted[i-1].Collection, sorted[i-1].Rkey) {
			return nil, fmt.Errorf("duplicate record path %s", path)
		}
		if err := w.WritePair([]byte(path), block.Bytes); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}
