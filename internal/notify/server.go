package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
)

// registrationTTL is how long a registerNotify subscription stays valid before
// the syncer must renew it.
const registrationTTL = 24 * time.Hour

// MembershipChecker reports whether a DID may read a space. It gates
// OAuth/service-auth registrations (a syncer holding a member's token rather
// than a space credential).
type MembershipChecker interface {
	IsMember(
		ctx context.Context,
		org syntax.DID,
		space habitat_syntax.SpaceURI,
		did syntax.DID,
	) (bool, error)
}

type Server struct {
	store   Store
	members MembershipChecker
	methods []authn.Method
}

// NewServer builds the notify server. methods are the accepted auth methods: a
// space credential authorizes exactly its space, while any other credential
// (OAuth / service auth) is authorized by a space-membership check.
func NewServer(store Store, members MembershipChecker, methods ...authn.Method) *Server {
	return &Server{store: store, members: members, methods: methods}
}

// RegisterNotify handles network.habitat.space.registerNotify: a syncer
// subscribes an endpoint to notifyWrite events for the whole space or a specific
// repo. The caller authenticates with a space credential, or with a member's
// OAuth/service-auth token (gated by a membership check).
func (s *Server) RegisterNotify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := authn.NewValidator(authn.WithAuthMethods(s.methods...)).Validate(w, r)
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

	if !s.authorize(ctx, w, credInfo, spaceURI) {
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

// authorize checks the credential may register for spaceURI: a space credential
// must name exactly that space; any other credential must belong to a member.
func (s *Server) authorize(
	ctx context.Context,
	w http.ResponseWriter,
	credInfo *authn.CredentialInfo,
	spaceURI habitat_syntax.SpaceURI,
) bool {
	if credInfo.Space != "" {
		if credInfo.Space != spaceURI {
			httpx.WriteInvalidRequest(ctx, w, "credential does not authorize this space",
				fmt.Errorf("credential space %q does not match %q", credInfo.Space, spaceURI))
			return false
		}
		return true
	}

	member, err := s.members.IsMember(ctx, credInfo.Org.DID(), spaceURI, credInfo.Subject)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("check membership: %w", err))
		return false
	}
	if !member {
		httpx.WriteSpaceNotFound(ctx, w, fmt.Errorf("not a member of %q", spaceURI))
		return false
	}
	return true
}
