package reverse_proxy

import (
	"fmt"

	"github.com/eagraf/habitat-new/core/state/node"
)

type RuleSet struct {
	rules        map[string]*node.ReverseProxyRule
	baseFilePath string // Optional, if set, all file server rules will be relative to this path
}

// AddRule is a wrapper around Add for finding the correct rule handler type.
func (rs RuleSet) AddRule(rule *node.ReverseProxyRule) error {
	if _, ok := rs.rules[rule.ID]; ok {
		return fmt.Errorf("reverse proxy rule with id %s is already present", rule.ID)
	}

	// Make sure the rule type is valid.
	if rule.Type != node.ProxyRuleRedirect && rule.Type != node.ProxyRuleFileServer && rule.Type != node.ProxyRuleEmbeddedFrontend {
		return fmt.Errorf("rule type %s is not supported", rule.Type)
	}

	// TODO we might need to make this threadsafe.
	rs.rules[rule.ID] = rule
	return nil
}

func (rs RuleSet) Remove(name string) error {
	if _, ok := rs.rules[name]; !ok {
		return fmt.Errorf("rule %s does not exist", name)
	}
	delete(rs.rules, name)
	return nil
}
