package permissions

import (
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bradenaw/juniper/xmaps"
)

type dummy struct {
	permsByNSID map[syntax.NSID]xmaps.Set[syntax.DID]
}

func (d *dummy) HasPermission(didstr string, nsid string, rkey string, write bool) (bool, error) {
	dids, ok := d.permsByNSID[syntax.NSID(nsid)]
	if !ok {
		return false, nil
	}
	return dids.Contains(syntax.DID(didstr)), nil
}

func (d *dummy) AddPermission(nsid string, didstr string) {
	dids, ok := d.permsByNSID[syntax.NSID(nsid)]
	if !ok {
		dids = make(xmaps.Set[syntax.DID])
		d.permsByNSID[syntax.NSID(nsid)] = dids
	}
	dids.Add(syntax.DID(didstr))
}

// NewDummyStore returns a permissions store that always returns true
func NewDummyStore() *dummy {
	return &dummy{
		permsByNSID: make(map[syntax.NSID]xmaps.Set[syntax.DID]),
	}
}
