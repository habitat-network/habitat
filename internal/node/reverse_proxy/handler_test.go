package reverse_proxy

import (
	"net/http/httputil"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetHandlerFromRule(t *testing.T) {
	redirectRule := &Rule{
		ID:     "redirect1",
		Type:   ProxyRuleRedirect,
		Target: "http://fake-target/api",
	}

	handler, err := getHandlerFromRule(redirectRule, "fake/web/bundle/path")
	require.Nil(t, err)

	if _, ok := handler.(*httputil.ReverseProxy); !ok {
		t.Errorf("expected handler to be a *httputil.ReverseProxy, got %T", handler)
	}
}
