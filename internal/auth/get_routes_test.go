package auth

import (
	"testing"

	"github.com/eagraf/habitat-new/internal/node/config"
	"github.com/gorilla/sessions"
	"github.com/stretchr/testify/require"
)

// Dumb test for coverage
func TestGetRoutes(t *testing.T) {
	testConfig, err := config.NewTestNodeConfig(nil)
	require.NoError(t, err)

	// Create a session store
	sessionStore := sessions.NewCookieStore([]byte("test-key"))

	// Call GetRoutes
	routes, err := GetRoutes(testConfig, sessionStore)
	require.NoError(t, err)

	require.Len(t, routes, 3)
}
