package did

import (
	"net/http"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/habitat-network/habitat/internal/httpx"
)

// didCtx is the @context of served DID documents: the DID Core context plus the
// verification method suites used by atproto and habitat.
var didCtx = []string{
	"https://www.w3.org/ns/did/v1",
	"https://w3id.org/security/multikey/v1",
	"https://w3id.org/security/suites/secp256k1-2019/v1",
}

// docWithContext wraps a DID document with the @context field required by the
// DID Core spec (identity.DIDDocument has no @context field).
type docWithContext struct {
	Context []string `json:"@context"`
	identity.DIDDocument
}

// handler serves a DID document as application/did+ld+json.
type handler struct {
	// doc is the DID document to serve.
	doc identity.DIDDocument
}

func NewHandler(doc identity.DIDDocument) *handler {
	return &handler{doc: doc}
}

// ServeHTTP implements http.Handler.
func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/did+ld+json")
	w.Header().Set("Cache-Control", "max-age=3600")
	httpx.WriteJSON(r.Context(), w, docWithContext{
		Context:     didCtx,
		DIDDocument: h.doc,
	})
}
