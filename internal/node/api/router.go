package api

import (
	"crypto/x509"
	"fmt"
	"net/http"

	"github.com/eagraf/habitat-new/internal/node/controller"
	"github.com/rs/zerolog"
)

type Route interface {
	http.Handler

	// Pattern reports the path at which this is registered.
	Pattern() string
	Method() string
}

type processedRoute struct {
	Route
}

func processRoute(route Route) processedRoute {
	return processedRoute{route}
}

func (p processedRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != p.Route.Method() {
		http.Error(
			w,
			fmt.Sprintf("invalid method, require %s", p.Route.Method()),
			http.StatusMethodNotAllowed,
		)
		return
	}
	p.Route.ServeHTTP(w, r)
}

func NewRouter(
	routes []Route,
	logger *zerolog.Logger,
	nodeController controller.NodeController,
	useTLS bool,
	rootUserCert *x509.Certificate,
) http.Handler {
	router := http.NewServeMux()
	for _, route := range routes {
		logger.Info().Msgf("Registering route: %s", route.Pattern())
		router.Handle(route.Pattern(), processRoute(route))
	}

	authMiddleware := &authenticationMiddleware{
		nodeController: nodeController,
		useTLS:         useTLS,
		rootUserCert:   rootUserCert,
	}

	return authMiddleware.Middleware(router)
}
