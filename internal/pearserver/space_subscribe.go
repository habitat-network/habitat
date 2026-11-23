package pearserver

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/spacescope"
	"github.com/habitat-network/habitat/internal/spacestream"
)

// Subscribe implements network.habitat.space.subscribe: a WebSocket stream of
// the record writes and space deletions this host holds, for every space the
// caller may read. See package spacestream.
func (p *PearServer) Subscribe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query, err := spacestream.ParseQuery(r.URL.Query())
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "invalid query params", err)
		return
	}
	cred, ok := p.validator.Request(
		authn.WithMethods(
			authn.ValidatorMethodOAuth,
			authn.ValidatorMethodServiceAuth,
			authn.ValidatorMethodSpaceCredential,
		),
	).Validate(w, r)
	if !ok {
		return
	}
	reader, ok := streamReader(cred)
	if !ok {
		httpx.WriteUnauthorized(ctx, w, "credential names no user or space", nil)
		return
	}
	sub, err := spacestream.Prepare(spacestream.Subscription{
		Query:     query,
		Reader:    reader,
		ExpiresAt: cred.ExpiresAt,
	})
	if errors.Is(err, spacestream.ErrCredentialMismatch) {
		httpx.WriteInvalidRequest(ctx, w, "invalid spaces filter", err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, err)
		return
	}
	// Once upgraded the response is the stream, so an error ending it can
	// only be logged.
	if err := p.spaceStream.ServeWebSocket(w, r, sub); err != nil {
		slog.WarnContext(ctx, "subscribe stream ended", "err", err)
	}
}

// streamReader is who a validated credential reads event streams as.
func streamReader(cred *authn.CredentialInfo) (spacestream.Reader, bool) {
	switch {
	case cred.Space != "":
		return spacestream.SpaceCredentialReader{Space: cred.Space}, true
	case cred.Subject != "" && cred.Method == authn.ValidatorMethodOAuth:
		// An OAuth client sees only the spaces its space: scopes cover,
		// however many the user can read.
		return spacestream.UserReader{
			DID:   cred.Subject,
			Scope: spacescope.FromScopes(cred.Scopes, cred.Subject),
		}, true
	case cred.Subject != "":
		return spacestream.UserReader{DID: cred.Subject, Scope: spacescope.Unrestricted{}}, true
	default:
		return nil, false
	}
}
