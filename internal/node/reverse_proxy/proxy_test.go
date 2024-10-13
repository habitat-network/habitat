package reverse_proxy

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/config"
	"github.com/eagraf/habitat-new/internal/node/logging"
	"github.com/stretchr/testify/require"
)

func TestProxy(t *testing.T) {
	// Simulate a server sitting behind the reverse proxy
	redirectedServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "Hello, World!")
		}),
	)
	defer redirectedServer.Close()

	redirectedServerURL, err := url.Parse(redirectedServer.URL)
	if err != nil {
		t.Fatalf(err.Error())
	}

	// Simulate a static file dir to be served
	fileDir := t.TempDir()
	defer os.RemoveAll(fileDir)

	file, err := os.CreateTemp(fileDir, "file")
	if err != nil {
		log.Fatalf(err.Error())
	}

	_, _ = file.Write([]byte("Hello, World!"))
	file.Close()

	// Create proxy server
	config, err := config.NewTestNodeConfig(nil)
	require.Nil(t, err)
	proxy := NewProxyServer(logging.NewLogger(), config)
	err = proxy.RuleSet.AddRule(&node.ReverseProxyRule{
		ID:      "backend1",
		Type:    node.ProxyRuleRedirect,
		Matcher: "/backend1",
		Target:  redirectedServerURL.String(),
	})
	require.Nil(t, err)

	// Test adding naming conflict
	err = proxy.RuleSet.AddRule(&node.ReverseProxyRule{
		ID:      "backend1",
		Type:    node.ProxyRuleRedirect,
		Matcher: "/backend1",
		Target:  redirectedServerURL.String(),
	})
	require.NotNil(t, err)

	err = proxy.RuleSet.AddRule(&node.ReverseProxyRule{
		ID:      "fileserver",
		Type:    node.ProxyRuleFileServer,
		Matcher: "/fileserver",
		Target:  fileDir,
	})
	require.Nil(t, err)
	require.Equal(t, 2, len(proxy.RuleSet.rules))

	// binding to :0 chooses any open ports for tests
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.Nil(t, err)
	port := listener.Addr().(*net.TCPAddr).Port

	go func() {
		_ = http.Serve(listener, proxy)
	}()

	url := fmt.Sprintf("http://localhost:%d", port)
	// Check redirection
	resp, err := http.Get(url + "/backend1")
	if err != nil {
		t.Errorf(err.Error())
	}

	require.Equal(t, http.StatusOK, resp.StatusCode)

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf(err.Error())
	}

	require.Equal(t, "Hello, World!", string(b))

	// Check file serving
	resp, err = http.Get(fmt.Sprintf("%s/fileserver/%s", url, filepath.Base(file.Name())))
	if err != nil {
		t.Errorf(err.Error())
	}

	require.Equal(t, http.StatusOK, resp.StatusCode)

	b, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf(err.Error())
	}

	require.Equal(t, "Hello, World!", string(b))

	// Check getting a file that doesn't exist
	resp, err = http.Get(fmt.Sprintf("%s/fileserver/%s", url, "nonexistentfile"))
	if err != nil {
		t.Errorf(err.Error())
	}

	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	// Test the embedded frontend
	err = proxy.RuleSet.AddRule(&node.ReverseProxyRule{
		ID:      "frontend-rule",
		Type:    node.ProxyRuleEmbeddedFrontend,
		Matcher: "",
	})
	require.Nil(t, err)
	resp, err = http.Get(url)
	if err != nil {
		t.Error(err.Error())
	}
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Test removing a rule
	err = proxy.RuleSet.Remove("fileserver")
	require.Nil(t, err)
	resp, err = http.Get(fmt.Sprintf("%s/fileserver/%s", url, "nonexistentfile"))
	require.Nil(t, err)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Equal(t, 2, len(proxy.RuleSet.rules))

	// Removing it again should fail
	err = proxy.RuleSet.Remove("fileserver")
	require.NotNil(t, err)

	require.Equal(t, 2, len(proxy.RuleSet.rules))
}

func TestAddRule(t *testing.T) {
	proxy := &ProxyServer{
		RuleSet: &RuleSet{
			rules: make(map[string]*node.ReverseProxyRule),
		},
	}

	err := proxy.RuleSet.AddRule(&node.ReverseProxyRule{
		ID:      "backend1",
		Type:    "redirect",
		Matcher: "/backend1",
		Target:  "http://localhost:8080",
	})
	require.Nil(t, err)
	require.Equal(t, 1, len(proxy.RuleSet.rules))
	require.Equal(t, node.ProxyRuleRedirect, proxy.RuleSet.rules["backend1"].Type)

	err = proxy.RuleSet.AddRule(&node.ReverseProxyRule{
		ID:      "backend2",
		Type:    "file",
		Matcher: "/backend2",
		Target:  "http://localhost:8080",
	})
	require.Nil(t, err)
	require.Equal(t, 2, len(proxy.RuleSet.rules))
	require.Equal(t, node.ProxyRuleFileServer, proxy.RuleSet.rules["backend2"].Type)

	// Test unknown rule
	err = proxy.RuleSet.AddRule(&node.ReverseProxyRule{
		ID:      "backend3",
		Type:    "unknown",
		Matcher: "/backend3",
		Target:  "http://localhost:8080",
	})
	require.NotNil(t, err)
}
