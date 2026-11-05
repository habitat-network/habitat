package pearapp_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gocloud.dev/blob/memblob"

	"github.com/habitat-network/habitat/internal/pearapp"
)

func TestNewHoldsItsDependencies(t *testing.T) {
	bucket := memblob.OpenBucket(nil)
	t.Cleanup(func() { _ = bucket.Close() })

	app := pearapp.New(
		nil, // database
		nil, // instance admin store
		nil, // instance admin server
		"pear.example.com",
		"hive.example.com",
		"mcp.example.com",
		nil, // oauth client
		nil, // hive
		nil, // default directory
		nil, // hive directory
		nil, // pds client factory
		nil, // email domain store
		nil, // host key
		nil, // host KEM key
		nil, // notify store
		nil, // notifier
		bucket,
		nil, // blob store
		nil, // spaces store
		nil, // opensocial store
		nil, // email resolver
		nil, // oauth server
		nil, // service auth
		nil, // perm store
		nil, // validator
		nil, // credential revocations
		nil, // delegation tokens spent
		nil, // simple store
		nil, // nango client
		nil, // mcp gateway store
		nil, // llm store
		nil, // pds forwarding
		nil, // client metadata
		nil, // search index
		nil, // search indexer
		nil, // searcher
		nil, // search config store
	)

	require.Equal(t, "pear.example.com", app.Domain)
	require.Equal(t, "hive.example.com", app.HiveDomain)
	require.Equal(t, "mcp.example.com", app.MCPOrigin)
	require.Same(t, bucket, app.BlobBucket)
	require.NoError(t, app.Close())
}
