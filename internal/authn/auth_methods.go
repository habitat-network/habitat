package authn

import (
	"context"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type Method interface {
	CanHandle(r *http.Request) bool
	Validate(w http.ResponseWriter, r *http.Request, scopes ...string) (syntax.DID, bool)
	ValidateRaw(ctx context.Context, token string, scopes ...string) (syntax.DID, bool, error)
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
	w.WriteHeader(http.StatusUnauthorized)
	return "", false
}

func ValidateRaw(
	ctx context.Context,
	token string,
	/* TODO: take in scopes here */
	authMethod ...Method,
) (syntax.DID, bool, error) {
	var err error
	var did syntax.DID
	var ok bool
	for _, method := range authMethod {
		did, ok, err = method.ValidateRaw(ctx, token /* TODO: scopes */)
		// Allow first pass through
		if ok && err == nil {
			return did, true, nil
		}
	}
	// Return the last error
	return did, ok, fmt.Errorf("no auth method passed; latest err: %w", err)
}
