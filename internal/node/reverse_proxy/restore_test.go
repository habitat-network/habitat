package reverse_proxy

import (
	"testing"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/core/state/node/test_helpers"
	"github.com/stretchr/testify/assert"
)

func TestProcessRestorer(t *testing.T) {
	ruleSet := make(RuleSet)

	restorer := &ReverseProxyRestorer{
		ruleSet: ruleSet,
	}

	restoreUpdate, err := test_helpers.StateUpdateTestHelper(&node.InitalizationTransition{}, &node.State{
		Users: map[string]*node.User{
			"user1": {
				ID: "user1",
			},
		},
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
		Processes: map[string]*node.ProcessState{
			"proc1": {
				Process: &node.Process{
					ID:    "proc1",
					AppID: "app1",
				},
				State: node.ProcessStateRunning,
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
			"brokenrule": {
				ID:      "rule1",
				AppID:   "app1",
				Type:    "unknown",
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
	})
	assert.Nil(t, err)

	err = restorer.Restore(restoreUpdate)
	assert.Nil(t, err)

	assert.Equal(t, 1, len(restorer.ruleSet))
}
