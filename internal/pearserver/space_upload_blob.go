package pearserver

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/atdata"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
)

func (p *PearServer) UploadBlob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth),
	).Validate(w, r)
	if !ok {
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
	c, size, err := p.blobStore.PutBlob(ctx, mimeType, data)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("store blob: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatRepoUploadBlobOutput{
		Cid: c.String(),
		Blob: atdata.Blob{
			Ref:      atdata.CIDLink(c),
			MimeType: mimeType,
			Size:     size,
		},
	})
}
