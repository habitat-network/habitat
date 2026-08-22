package pearsetup

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/stretchr/testify/require"
)

func TestConfigDefaults(t *testing.T) {
	c := Config{Domain: "pear.example.com"}.withDefaults()

	require.Equal(t, "pear.example.com", c.HiveDomain, "hive domain falls back to domain")
	require.Equal(t, "https://pear.example.com", c.PDSOAuthClientURI, "client URI falls back to domain")
	require.Equal(t, "8000", c.Port)
	require.NotNil(t, c.Directory, "directory defaults to the network directory")
}

func TestConfigDefaultsKeepExplicitValues(t *testing.T) {
	dir := identity.NewMockDirectory()
	c := Config{
		Domain:            "pear.example.com",
		HiveDomain:        "members.example.com",
		PDSOAuthClientURI: "tunnel.example.com",
		Port:              "9999",
		Directory:         dir,
	}.withDefaults()

	require.Equal(t, "members.example.com", c.HiveDomain)
	require.Equal(t, "https://tunnel.example.com", c.PDSOAuthClientURI)
	require.Equal(t, "9999", c.Port)
	require.Equal(t, dir, c.Directory)
}

func TestConfigValidateRequiresDomain(t *testing.T) {
	err := Config{DB: "sqlite://x"}.withDefaults().validate()
	require.ErrorContains(t, err, "domain")
}
