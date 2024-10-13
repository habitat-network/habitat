package reverse_proxy

import (
	"testing"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/core/state/node/test_helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartProcessExecutor(t *testing.T) {
	executor := &ProcessProxyRulesExecutor{
		RuleSet: &RuleSet{
			rules: make(map[string]*node.ReverseProxyRule),
		},
	}

	startProcessStateUpdate, err := test_helpers.StateUpdateTestHelper(&node.ProcessStartTransition{
		AppID: "app1",
	}, &node.State{
		AppInstallations: map[string]*node.AppInstallationState{
			"app1": {
				AppInstallation: &node.AppInstallation{
					ID:   "app1",
					Name: "appname1",
					Package: node.Package{
						Driver: "test",
					},
				},
			},
		},
		ReverseProxyRules: &map[string]*node.ReverseProxyRule{
			"rule1": {
				ID:      "rule1",
				AppID:   "app1",
				Type:    "redirect",
				Matcher: "/path",
				Target:  "http://localhost:8080",
			},
			"rule2": {
				ID:      "rule2",
				AppID:   "app2",
				Type:    "redirect",
				Matcher: "/path2",
				Target:  "http://localhost:8080",
			},
		},
		Processes: map[string]*node.ProcessState{},
	})
	assert.Nil(t, err)

	shouldExecute, err := executor.ShouldExecute(startProcessStateUpdate)
	assert.Nil(t, err)
	assert.Equal(t, true, shouldExecute)
	assert.Equal(t, 0, len(executor.RuleSet.rules))

	err = executor.Execute(startProcessStateUpdate)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(executor.RuleSet.rules))
}

func TestBrokenRule(t *testing.T) {
	executor := &ProcessProxyRulesExecutor{
		RuleSet: &RuleSet{
			rules: make(map[string]*node.ReverseProxyRule),
		},
	}

	startProcessStateUpdate, err := test_helpers.StateUpdateTestHelper(&node.ProcessStartTransition{
		AppID: "app1",
	}, &node.State{
		AppInstallations: map[string]*node.AppInstallationState{
			"app1": {
				AppInstallation: &node.AppInstallation{
					ID:   "app1",
					Name: "appname1",
					Package: node.Package{
						Driver: "test",
					},
				},
			},
		},
		ReverseProxyRules: &map[string]*node.ReverseProxyRule{
			"brokenrule": {
				ID:      "rule1",
				AppID:   "app1",
				Type:    "unknown",
				Matcher: "/path",
				Target:  "http://localhost:8080",
			},
		},
		Processes: map[string]*node.ProcessState{},
	})
	assert.Nil(t, err)

	err = executor.Execute(startProcessStateUpdate)
	assert.NotNil(t, err)
	assert.Equal(t, 0, len(executor.RuleSet.rules))
}

func TestAddRuleExecutor(t *testing.T) {
	subscriber, err := NewProcessProxyRuleSubscriber(
		&RuleSet{
			rules: make(map[string]*node.ReverseProxyRule),
		},
	)
	require.Nil(t, err)

	exec, err := subscriber.GetExecutor(node.TransitionAddReverseProxyRule)
	require.Nil(t, err)
	executor, _ := exec.(*AddProxyRulesExecutor)

	addRuleStateUpdate, err := test_helpers.StateUpdateTestHelper(&node.AddReverseProxyRuleTransition{
		Rule: &node.ReverseProxyRule{
			ID:      "new-rule",
			Type:    node.ProxyRuleRedirect,
			Matcher: "/my-matcher",
			Target:  "http://myhost/api",
		},
	}, &node.State{
		ReverseProxyRules: &map[string]*node.ReverseProxyRule{},
	})
	assert.Nil(t, err)

	shouldExecute, err := executor.ShouldExecute(addRuleStateUpdate)
	assert.Nil(t, err)
	assert.Equal(t, true, shouldExecute)
	assert.Equal(t, 0, len(executor.RuleSet.rules))

	err = executor.Execute(addRuleStateUpdate)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(executor.RuleSet.rules))

}
