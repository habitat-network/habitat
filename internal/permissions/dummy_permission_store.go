package permissions

import (
	"errors"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bradenaw/juniper/xmaps"
)

type dummy struct {
	permsByNSID map[string] /* owner */ map[syntax.NSID]xmaps.Set[syntax.DID] /* grantee */
}

var _ Store = (*dummy)(nil)

func (d *dummy) HasPermission(
	requester string,
	owner string,
	nsid string,
	rkey string,
) (bool, error) {
	dids, ok := d.permsByNSID[owner][syntax.NSID(nsid)]
	if !ok {
		return false, nil
	}
	return dids.Contains(syntax.DID(requester)), nil
}

func (d *dummy) AddLexiconReadPermission(grantee string, owner string, nsid string) error {
	dids, ok := d.permsByNSID[owner][syntax.NSID(nsid)]
	if !ok {
		dids = make(xmaps.Set[syntax.DID])
		if d.permsByNSID[owner] == nil {
			d.permsByNSID[owner] = make(map[syntax.NSID]xmaps.Set[syntax.DID])
		}
		d.permsByNSID[owner][syntax.NSID(nsid)] = dids
	}
	dids.Add(syntax.DID(grantee))
	return nil
}

func (d *dummy) RemoveLexiconReadPermission(
	grantee string,
	owner string,
	nsid string,
) error {
	return errors.ErrUnsupported
}

func (d *dummy) ListReadPermissionsByLexicon(owner string) (map[string][]string, error) {
	return nil, errors.ErrUnsupported
}

// NewDummyStore returns a permissions store that always returns true
func NewDummyStore() *dummy {
	return &dummy{
		permsByNSID: make(map[string]map[syntax.NSID]xmaps.Set[syntax.DID]),
	}
}
