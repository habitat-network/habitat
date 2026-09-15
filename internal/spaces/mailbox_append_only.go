package spaces

import (
	"errors"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

var ErrImmutableRecord = errors.New("mailbox record is immutable")

// These five Comail collections form an append-only mailbox graph. Changes
// create new revisions and logical tombstones. The policy lives at the store
// boundary so both ordinary writes and atomic creates retain operation claims.
func immutableMailboxCollection(collection syntax.NSID) bool {
	switch collection {
	case "email.atmos.message",
		"email.atmos.messageStateRevision",
		"email.atmos.messageStateOperation",
		"email.atmos.folderRevision",
		"email.atmos.folderOperation":
		return true
	default:
		return false
	}
}
