package testutil

import (
	"context"

	"github.com/bluesky-social/indigo/atproto/syntax"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

type TestNotifier struct {
	Writes  []writeCall
	Deleted []habitat_syntax.SpaceURI
}

type writeCall struct {
	Space habitat_syntax.SpaceURI
	Repo  syntax.DID
	Rev   syntax.TID
}

func (n *TestNotifier) NotifyWrite(
	_ context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	rev syntax.TID,
) {
	n.Writes = append(n.Writes, writeCall{Space: space, Repo: repo, Rev: rev})
}

func (n *TestNotifier) NotifySpaceDeleted(
	_ context.Context,
	space habitat_syntax.SpaceURI,
) {
	n.Deleted = append(n.Deleted, space)
}
