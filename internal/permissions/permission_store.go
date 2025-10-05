package permissions

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
)

type Store interface {
	HasPermission(
		requester string,
		owner string,
		nsid string,
		rkey string,
	) (bool, error)
	AddLexiconReadPermission(
		grantee string,
		owner string,
		nsid string,
	) error
	RemoveLexiconReadPermission(
		grantee string,
		owner string,
		nsid string,
	) error
	ListReadPermissionsByLexicon(owner string) (map[string][]string, error)
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
	requester string,
	owner string,
	nsid string,
	rkey string,
) (bool, error) {
	return p.enforcer.Enforce(requester, owner, getCasbinObjectFromRecord(nsid, rkey))
}

// TODO: do some validation on input, possible cases:
// - duplicate policies
// - conflicting policies
func (p *casbinStore) AddLexiconReadPermission(
	requester string,
	owner string,
	nsid string,
) error {
	_, err := p.enforcer.AddPolicy(requester, owner, getCasbinObjectFromLexicon(nsid), "allow")
	if err != nil {
		return err
	}
	return p.adapter.SavePolicy(p.enforcer.GetModel())
}

// TODO: do some validation on input
func (p *casbinStore) RemoveLexiconReadPermission(
	requester string,
	owner string,
	nsid string,
) error {
	// TODO: should we actually be adding a deny here instead of just removing allow?
	_, err := p.enforcer.RemovePolicy(
		requester,
		owner,
		getCasbinObjectFromLexicon(nsid),
		"allow",
	)
	if err != nil {
		return err
	}
	return p.adapter.SavePolicy(p.enforcer.GetModel())
}

func (p *casbinStore) ListReadPermissionsByLexicon(owner string) (map[string][]string, error) {
	policies, err := p.enforcer.GetFilteredPolicy(1, owner)
	if err != nil {
		return nil, err
	}

	res := make(map[string][]string)
	for _, policy := range policies {
		lexicon := strings.TrimSuffix(policy[2], ".*")
		// ignore denies for now
		if policy[3] == "allow" {
			res[lexicon] = append(res[lexicon], policy[0])
		}
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
