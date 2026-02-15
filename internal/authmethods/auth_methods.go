package authmethods

import (
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type Method interface {
	CanHandle(r *http.Request) bool
	Validate(w http.ResponseWriter, r *http.Request, scopes ...string) (syntax.DID, bool)
}

func Validate(
	w http.ResponseWriter,
	r *http.Request,
	authMethod ...Method,
) (syntax.DID, bool) {
	for _, method := range authMethod {
		if method.CanHandle(r) {
			return method.Validate(w, r)
		}
	}
	return "", false
}
