package notify

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/utils"
)

// registrationTTL is how long a registerNotify subscription stays valid before
// the syncer must renew it.
const registrationTTL = 24 * time.Hour

type Server struct {
	store     Store
	spaceCred authn.Method
}

func NewServer(store Store, spaceCred authn.Method) *Server {
	return &Server{store: store, spaceCred: spaceCred}
}

// RegisterNotify handles network.habitat.space.registerNotify: a syncer
// authenticated with a space credential subscribes an endpoint to notifyWrite
// events for the whole space or a specific repo.
func (s *Server) RegisterNotify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := authn.NewValidator(authn.WithAuthMethods(s.spaceCred)).Validate(w, r)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatSpaceRegisterNotifyInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.LogAndHTTPError(ctx, w, err, "decode request body", http.StatusBadRequest)
		return
	}

	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}

	// The space credential must authorize the space being registered against.
	if credInfo.Space != spaceURI {
		httpx.WriteInvalidRequest(ctx, w, "credential does not authorize this space",
			fmt.Errorf("credential space %q does not match %q", credInfo.Space, spaceURI))
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
		utils.LogAndHTTPError(ctx, w, err, "register notify", http.StatusInternalServerError)
		return
	}

	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSpaceRegisterNotifyOutput{
		ExpiresAt: expiresAt.UTC().Format(time.RFC3339),
	})
}
