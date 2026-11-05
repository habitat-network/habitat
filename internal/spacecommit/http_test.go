package spacecommit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommitXRPCRoundTrip(t *testing.T) {
	c := New(refSpace, refRepo, refRev, refData)

	api := c.ToXRPC()
	require.Equal(t, int64(Version), api.Ver)
	require.Equal(t, refRepo.String(), api.Repo)
	require.Equal(t, refSpace.String(), api.Space)
	require.Equal(t, refRev, api.Rev)
	require.Equal(t, refData, []byte(api.Data))
	require.Equal(t, c, FromXRPC(api))

	com := c.ToComAtprotoXRPC()
	require.Equal(t, api.Repo, com.Repo)
	require.Equal(t, api.Space, com.Space)
	require.Equal(t, []byte(api.Data), []byte(com.Data))
}
