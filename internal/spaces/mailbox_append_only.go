package spaces

import (
	"errors"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

var ErrImmutableRecord = errors.New("mailbox record is immutable")

// Mailbox records are append-only. State changes use revisions and tombstones
// so ordinary writes and atomic creates preserve operation claims.
func immutableMailboxCollection(collection syntax.NSID) bool {
	switch collection {
	case "email.atmos.message",
		"email.atmos.messageStateRevision",
		"email.atmos.messageStateOperation",
		"email.atmos.folderRevision",
		"email.atmos.folderOperation",
		"email.atmos.messagesPrototype.entry":
		return true
	default:
		return false
	}
}
