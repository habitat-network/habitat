package testutil_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/pearsetup/testutil"
)

func TestNewServesHealth(t *testing.T) {
	p := testutil.New(t)

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	resp := p.Do(nil, req)

	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestNewBuildsInstance(t *testing.T) {
	p := testutil.New(t)

	require.NotNil(t, p.OrgStore)
	require.NotNil(t, p.OAuthServer)
	require.Equal(t, testutil.Domain, p.Config.Domain)
}
