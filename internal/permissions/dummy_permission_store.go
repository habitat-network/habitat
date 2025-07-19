package permissions

import (
	"errors"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bradenaw/juniper/xmaps"
)

type dummy struct {
	permsByNSID map[syntax.NSID]xmaps.Set[syntax.DID]
}

func (d *dummy) HasPermission(didstr string, nsid string, rkey string) (bool, error) {
	dids, ok := d.permsByNSID[syntax.NSID(nsid)]
	if !ok {
		return false, nil
	}
	return dids.Contains(syntax.DID(didstr)), nil
}

func (d *dummy) AddLexiconReadPermission(nsid string, didstr string) error {
	dids, ok := d.permsByNSID[syntax.NSID(nsid)]
	if !ok {
		dids = make(xmaps.Set[syntax.DID])
		d.permsByNSID[syntax.NSID(nsid)] = dids
	}
	dids.Add(syntax.DID(didstr))
	return nil
}

func (d *dummy) RemoveLexiconReadPermission(
	didstr string,
	nsid string,
) error {
	return errors.ErrUnsupported
}
func (d *dummy) ListReadPermissionsByLexicon() (map[string][]string, error) {
	return nil, errors.ErrUnsupported
}

// NewDummyStore returns a permissions store that always returns true
func NewDummyStore() *dummy {
	return &dummy{
		permsByNSID: make(map[syntax.NSID]xmaps.Set[syntax.DID]),
	}
}
