package authn

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"
)

// Make sure the key used for setting the callerDID in the context is unique;
// an unexported type means no collisions with other packages.
type callerCtxKey struct{}

var callerKey = callerCtxKey{}

func Middleware(authMethods ...Method) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callerDID, ok := Validate(w, r, authMethods...)
			if !ok {
				return
			}

			ctx := context.WithValue(r.Context(), callerKey, callerDID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
