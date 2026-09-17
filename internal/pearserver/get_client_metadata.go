package pearserver

import (
	"net/http"

	"github.com/habitat-network/habitat/internal/httpx"
)

// GetClientMetadata fetches and returns a client's OAuth client-id-metadata
// document (https://atproto.com/specs/oauth#client-id-metadata-document)
// server-side, so the management frontend can render a client's name, logo,
// and scopes without depending on the client's own server sending CORS
// headers. Public: metadata documents are public by nature, and the document
// URL is the client_id itself. Localhost client_ids are synthesized rather
// than fetched (see clientmetadata.Resolver.FetchMetadata).
func (p *PearServer) GetClientMetadata(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	clientID := r.URL.Query().Get("client_id")
	if clientID == "" {
		httpx.WriteInvalidRequest(ctx, w, "missing client_id", nil)
		return
	}
	metadata, err := p.clientMeta.FetchMetadata(ctx, clientID)
	if err != nil {
		httpx.WriteError(
			ctx, w, "BadGateway",
			"failed to fetch client metadata: "+err.Error(),
			http.StatusBadGateway,
		)
		return
	}
	httpx.WriteJSON(ctx, w, metadata)
}
