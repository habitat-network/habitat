package did

import (
	"encoding/json"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/identity"
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

// Handler serves a DID document as application/did+ld+json.
type Handler struct {
	// Doc is the DID document to serve.
	Doc identity.DIDDocument
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/did+ld+json")
	w.Header().Set("Cache-Control", "max-age=3600")
	err := json.NewEncoder(w).Encode(docWithContext{
		Context:     didCtx,
		DIDDocument: h.Doc,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
