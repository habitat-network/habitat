package testutil

import (
	"context"

	"github.com/bluesky-social/indigo/atproto/syntax"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

type TestNotifier struct {
	Writes              []writeCall
	Deleted             []habitat_syntax.SpaceURI
	RegisteredAuthority []authorityCall
}

type authorityCall struct {
	Space habitat_syntax.SpaceURI
	Repo  syntax.DID
}

type writeCall struct {
	Space habitat_syntax.SpaceURI
	Repo  syntax.DID
	Rev   syntax.TID
	Hash  []byte
}

func (n *TestNotifier) NotifyWrite(
	_ context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	rev syntax.TID,
	hash []byte,
) {
	n.Writes = append(n.Writes, writeCall{Space: space, Repo: repo, Rev: rev, Hash: hash})
}

func (n *TestNotifier) NotifySpaceDeleted(
	_ context.Context,
	space habitat_syntax.SpaceURI,
) {
	n.Deleted = append(n.Deleted, space)
}

func (n *TestNotifier) RegisterAuthority(
	_ context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
) {
	n.RegisteredAuthority = append(n.RegisteredAuthority, authorityCall{Space: space, Repo: repo})
}
