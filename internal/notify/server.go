package notify

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/httpx"
)

// registrationTTL is how long a registerNotify subscription stays valid before
// the syncer must renew it.
const registrationTTL = 24 * time.Hour

type Server struct {
	store     Store
	validator authn.RequestValidator
}

func NewServer(store Store, validator authn.RequestValidator) *Server {
	return &Server{store: store, validator: validator}
}

// RegisterNotify handles network.habitat.space.registerNotify: a syncer
// subscribes an endpoint to notifyWrite events for the whole space or a
// specific repo. The caller authenticates with a space credential naming that
// space.
func (s *Server) RegisterNotify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSpaceRegisterNotifyInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	// The space credential must authorize the space being registered against.
	if _, ok = s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodSpaceCredential),
		authn.WithSpace(spaceURI, fgastore.RelationSpaceReader),
	).Validate(w, r); !ok {
		return
	}
	var repo syntax.DID
	if input.Repo != "" {
		repo, ok = httpx.ParseDIDInput(ctx, w, input.Repo, "repo")
		if !ok {
			return
		}
	}
	expiresAt := time.Now().Add(registrationTTL)
	if err := s.store.Register(ctx, spaceURI, repo, input.Endpoint, expiresAt); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("register notify: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSpaceRegisterNotifyOutput{
		ExpiresAt: expiresAt.UTC().Format(time.RFC3339),
	})
}
