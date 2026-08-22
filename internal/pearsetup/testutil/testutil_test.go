package testutil_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/pearsetup/testutil"
)

func TestNewBuildsInstance(t *testing.T) {
	p := testutil.New(t)

	require.NotNil(t, p.OrgStore)
	require.NotNil(t, p.OAuthServer)
	require.Equal(t, testutil.Domain, p.Config.Domain)
}
