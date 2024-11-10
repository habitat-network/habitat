package appstore

import (
	"bytes"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"

	"text/template"

	types "github.com/eagraf/habitat-new/core/api"
	"github.com/eagraf/habitat-new/internal/node/config"
	"github.com/eagraf/habitat-new/internal/node/constants"
	yaml "gopkg.in/yaml.v3"
)

//For development, we just embed a default list of apps that is helpful for working
// on the Habitat node. These apps are templated, so that system specific values can
// be inerted at runtime using the text/template package.

//go:embed apps.dev.yml.tpl
var appsDevYml embed.FS

// getAppsList returns the contents of the embedded apps.dev.yml file
func getDevAppsList(config *config.NodeConfig) ([]*types.PostAppRequest, error) {
	raw, err := fs.ReadFile(appsDevYml, "apps.dev.yml.tpl")
	if err != nil {
		return nil, err
	}

	return renderDevAppsList(config, raw)
}

// Render the template for the dev apps list. Only used in dev mode.
func renderDevAppsList(config *config.NodeConfig, raw []byte) ([]*types.PostAppRequest, error) {
	tmpl, err := template.New("apps").Parse(string(raw))
	if err != nil {
		return nil, err
	}

	data := struct {
		HabitatPath string
	}{
		HabitatPath: config.HabitatPath(),
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	if err != nil {
		return nil, err
	}

	yml := buf.Bytes()

	var appsList []*types.PostAppRequest
	err = yaml.Unmarshal(yml, &appsList)
	if err != nil {
		return nil, err
	}

	return appsList, nil
}

// AvailableAppsRoute lists apps the user is able to install.
type AvailableAppsRoute struct {
	config *config.NodeConfig
}

func NewAvailableAppsRoute(config *config.NodeConfig) *AvailableAppsRoute {
	return &AvailableAppsRoute{config: config}
}

func (h *AvailableAppsRoute) Pattern() string {
	return "/app_store/available_apps"
}

func (h *AvailableAppsRoute) Method() string {
	return http.MethodGet
}

func (h *AvailableAppsRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.config.Environment() == constants.EnvironmentDev {
		apps, err := getDevAppsList(h.config)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		marsahalled, err := json.Marshal(apps)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		_, err = w.Write(marsahalled)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		// TODO: implement this in "prod" mode
		http.Error(w, "Not implemented", http.StatusNotImplemented)
	}
}
