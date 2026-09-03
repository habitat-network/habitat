package pearserver

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
)

// UploadImage implements community.opensocial.uploadImage: sets the
// community's profile avatar from the uploaded image bytes. Requires the
// caller to be an admin of the community.
func (p *PearServer) UploadImage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var params opensocial_api.CommunityOpensocialUploadImageParams
	if err := p.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode query params", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, params.Org, "org")
	if !ok {
		return
	}
	if !p.requireAdmin(ctx, w, org, credInfo.Subject) {
		return
	}
	mimeType := r.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 500*1024))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		httpx.WriteError(ctx, w, "BlobTooLarge", "max 500kb", http.StatusRequestEntityTooLarge)
		return
	} else if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to read request body", err)
		return
	}
	blob, err := p.opensocialStore.UploadImage(ctx, org, mimeType, data)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("upload image: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, opensocial_api.CommunityOpensocialUploadImageOutput{Blob: blob})
}
