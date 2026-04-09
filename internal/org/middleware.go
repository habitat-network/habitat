package org

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/utils"
)

func Middleware(mgr Manager) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			caller, ok := authn.FromContext(r.Context())
			if !ok {
				return
			}

			ok, err := mgr.isMember(r.Context(), caller)
			if err != nil {
				utils.LogAndHTTPError(w, err, "checking org isMember", http.StatusInternalServerError)
			}

			if !ok {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
