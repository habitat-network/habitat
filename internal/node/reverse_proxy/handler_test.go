package reverse_proxy

import (
	"net/http/httputil"
	"testing"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/config"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestGetHandlerFromRule(t *testing.T) {
	v := viper.New()

	fakeConfig, err := config.NewNodeConfigFromViper(v)
	if err != nil {
		t.Error(err)
	}

	redirectRule := &node.ReverseProxyRule{
		ID:     "redirect1",
		Type:   node.ProxyRuleRedirect,
		Target: "http://fake-target/api",
	}

	handler, err := getHandlerFromRule(redirectRule, fakeConfig)
	require.Nil(t, err)

	if _, ok := handler.(*httputil.ReverseProxy); !ok {
		t.Errorf("expected handler to be a *httputil.ReverseProxy, got %T", handler)
	}
}
