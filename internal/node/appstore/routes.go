package appstore

import (
	"bytes"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"

	"text/template"

	types "github.com/eagraf/habitat-new/core/api"
	yaml "gopkg.in/yaml.v3"
)

//For development, we just embed a default list of apps that is helpful for working
// on the Habitat node. These apps are templated, so that system specific values can
// be inerted at runtime using the text/template package.

//go:embed apps.dev.yml.tpl
var appsDevYml embed.FS

// getAppsList returns the contents of the embedded apps.dev.yml file
func getDevAppsList(path string) ([]*types.PostAppRequest, error) {
	raw, err := fs.ReadFile(appsDevYml, "apps.dev.yml.tpl")
	if err != nil {
		return nil, err
	}

	return renderDevAppsList(path, raw)
}

// Render the template for the dev apps list. Only used in dev mode.
func renderDevAppsList(path string, raw []byte) ([]*types.PostAppRequest, error) {
	tmpl, err := template.New("apps").Parse(string(raw))
	if err != nil {
		return nil, err
	}

	data := struct {
		HabitatPath string
	}{
		HabitatPath: path,
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
	path string
}

func NewAvailableAppsRoute(path string) *AvailableAppsRoute {
	return &AvailableAppsRoute{path}
}

func (h *AvailableAppsRoute) Pattern() string {
	return "/app_store/available_apps"
}

func (h *AvailableAppsRoute) Method() string {
	return http.MethodGet
}

func (h *AvailableAppsRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	apps, err := getDevAppsList(h.path)
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
}
