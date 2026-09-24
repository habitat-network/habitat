package pearserver

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"

	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
)

// GetSession implements com.atproto.server.getSession. For identities hosted
// on this instance (hive-managed) it responds with the caller's DID and handle
// resolved locally; for remote identities it forwards the request to their
// real PDS.
func (p *PearServer) GetSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	ident, err := p.hive.LookupDID(ctx, credInfo.Subject)
	if errors.Is(err, identity.ErrDIDNotFound) {
		p.pdsForwarding.ServeHTTP(w, r)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("resolving identity: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, atproto.ServerGetSession_Output{
		Did:    ident.DID.String(),
		Handle: ident.Handle.String(),
	})
}
