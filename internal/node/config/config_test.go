package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

const testHabitatYaml = `
default_apps:
  pouch_frontend:
    app_installation:
      name: pouch_frontend
      version: 3
      driver: web

      driver_config:
        download_url: https://github.com/eagraf/extension-save-to-pocket/releases/download/demo-release-2/dist.tar.gz
        bundle_directory_name: pouch

      registry_url_base: a
      registry_app_id: b
      registry_tag: c

    reverse_proxy_rules:
      - type: file
        matcher: /pouch
        target: pouch/3/dist
      - type: redirect
        matcher: /pouch_api
        target: http://100.113.121.9:5000
      - type: fishtail_ingest
        matcher: /pouch_api/ingest
        target: http://100.113.121.9:5000/api/v1/ingest
        fishtail_ingest_config:
          subscribed_collections:
            - lexicon: app.bsky.feed.like
            - lexicon: com.habitat.pouch.link

`

func TestLoadingDefaultApps(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")
	err := v.ReadConfig(strings.NewReader(testHabitatYaml))
	assert.NoError(t, err)

	testNodeConfig, err := NewNodeConfigFromViper(v)
	assert.NoError(t, err)

	defaultApps := testNodeConfig.DefaultApps()
	assert.Len(t, defaultApps, 1)
	assert.Equal(t, "pouch_frontend", defaultApps[0].AppInstallation.Name)
	assert.Equal(t, "pouch/3/dist", defaultApps[0].ReverseProxyRules[0].Target)
	assert.Len(t, defaultApps[0].ReverseProxyRules, 3)
}
