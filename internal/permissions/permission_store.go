package permissions

import (
	_ "embed"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/bradenaw/juniper/xmaps"
	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
)

// enum defining the possible actions a user has permission to do on an object.
// an object is either a lexicon or a lexicon + record key
type Action int

const (
	// We do not support Write permissions at this time. The PDS already enforces that only the logged-in user associated with a
	// DID can write data to the repo.
	Read Action = iota
)

var actionNames = map[Action]string{
	Read: "read",
}

func (a Action) String() string {
	return actionNames[a]
}

type Store interface {
	HasPermission(
		didstr string,
		nsid string,
		rkey string,
	) (bool, error)
	AddLexiconReadPermission(
		didstr string,
		nsid string,
	) error
	RemoveLexiconReadPermission(
		didstr string,
		nsid string,
	) error
	ListReadPermissionsByLexicon() (map[string][]string, error)
}

type casbinStore struct {
	enforcer *casbin.Enforcer
	adapter  persist.Adapter
}

//go:embed model.conf
var modelStr string

func NewStore(adapter persist.Adapter, autoSave bool) (Store, error) {
	m, err := model.NewModelFromString(modelStr)
	if err != nil {
		return nil, err
	}
	enforcer, err := casbin.NewEnforcer(m, adapter)
	if err != nil {
		return nil, err
	}
	// Auto-Save allows for single policy updates to take effect dynamically.
	// https://casbin.org/docs/adapters/#autosave
	enforcer.EnableAutoSave(autoSave)
	return &casbinStore{
		enforcer: enforcer,
		adapter:  adapter,
	}, nil
}

// HasPermission implements PermissionStore.
// TODO: implement record key granularity for permissions
func (p *casbinStore) HasPermission(
	didstr string,
	nsid string,
	rkey string,
) (bool, error) {
	return p.enforcer.Enforce(didstr, getCasbinObjectFromRecord(nsid, rkey), Read.String())
}

// TODO: do some validation on input, possible cases:
// - duplicate policies
// - conflicting policies
func (p *casbinStore) AddLexiconReadPermission(
	didstr string,
	nsid string,
) error {
	_, err := p.enforcer.AddPolicy(didstr, getCasbinObjectFromLexicon(nsid), Read.String(), "allow")
	if err != nil {
		return err
	}
	return p.adapter.SavePolicy(p.enforcer.GetModel())
}

// TODO: do some validation on input
func (p *casbinStore) RemoveLexiconReadPermission(
	didstr string,
	nsid string,
) error {
	// TODO: should we actually be adding a deny here instead of just removing allow?
	_, err := p.enforcer.RemovePolicy(didstr, getCasbinObjectFromLexicon(nsid), Read.String(), "allow")
	if err != nil {
		return err
	}
	return p.adapter.SavePolicy(p.enforcer.GetModel())
}

func (p *casbinStore) ListReadPermissionsByLexicon() (map[string][]string, error) {
	objs, err := p.enforcer.GetAllObjects()
	if err != nil {
		return nil, err
	}

	res := make(map[string][]string)
	for _, obj := range objs {
		perms, err := p.enforcer.GetImplicitUsersForResource(obj)
		if err != nil {
			return nil, err
		}
		users := make(xmaps.Set[string], 0)
		for _, perm := range perms {
			// Format of perms is [[bob data2 write] [alice data2 read] [alice data2 write]]
			if perm[2] == Read.String() {
				users.Add(perm[0])
			}
		}
		res[strings.TrimSuffix(obj, ".*")] = slices.Collect(maps.Keys(users))
	}

	return res, nil
}

// Helpers to translate lexicon + record references into object type required by casbin
func getCasbinObjectFromRecord(lex string, rkey string) string {
	if rkey == "" {
		rkey = "*"
	}
	return fmt.Sprintf("%s.%s", lex, rkey)
}

func getCasbinObjectFromLexicon(lex string) string {
	return fmt.Sprintf("%s.*", lex)
}

// List all permissions (lexicon -> [](users | groups))
// Add a permission on a lexicon for a user or group
// Remove a permission on a lexicon for a user or group
