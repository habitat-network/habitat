package frontend

import (
	"embed"
	"io/fs"
	"net/url"

	"github.com/eagraf/habitat-new/internal/node/config"
	"github.com/eagraf/habitat-new/internal/node/reverse_proxy"
)

//go:embed build/*
var frontendBuild embed.FS

func NewFrontendProxyRule(config *config.NodeConfig) (reverse_proxy.RuleHandler, error) {
	if config.FrontendDev() {
		feDevServer, _ := url.Parse("http://habitat_frontend:8000/")
		// The root matcher is empty, so this rule will match all requests that don't have a more specific rule
		return &reverse_proxy.RedirectRule{
			Matcher:         "",
			ForwardLocation: feDevServer,
		}, nil
	} else {

		fSys, err := fs.Sub(frontendBuild, "build")
		if err != nil {
			return nil, err
		}
		return &reverse_proxy.FileServerRule{
			Matcher: "",
			FS:      fSys,
		}, nil
	}
}
