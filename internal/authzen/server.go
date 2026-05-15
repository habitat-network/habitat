package authzen

import (
	"encoding/json"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/pear"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/rs/zerolog/log"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

type authMethods struct {
	oauth       authn.Method
	serviceAuth authn.Method
}

type Server struct {
	authMethods authMethods
	pear        pear.Pear
}

func NewServer(pear pear.Pear, oauthMethod authn.Method, serviceAuthMethod authn.Method) *Server {
	return &Server{
		authMethods: authMethods{
			oauth:       oauthMethod,
			serviceAuth: serviceAuthMethod,
		},
		pear: pear,
	}
}

func (s *Server) EvaluateAccess(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := authn.Validate(w, r, s.authMethods.oauth, s.authMethods.serviceAuth)
	if !ok {
		return
	}

	var req habitat.NetworkHabitatAuthzenEvaluateInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.LogAndHTTPError(w, err, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Subject.Id == "" || req.Resource.Id == "" || req.Action.Name == "" {
		utils.LogAndHTTPError(w, nil, "subject.id, resource.id, and action.name are required", http.StatusBadRequest)
		return
	}

	requester, err := syntax.ParseDID(req.Subject.Id)
	if err != nil {
		utils.LogAndHTTPError(w, err, "invalid subject.id: must be a DID", http.StatusBadRequest)
		return
	}

	uri, err := habitat_syntax.ParseHabitatURI(req.Resource.Id)
	if err != nil {
		utils.LogAndHTTPError(w, err, "invalid resource.id: must be a habitat URI", http.StatusBadRequest)
		return
	}

	owner, collection, rkey, err := uri.ExtractParts()
	if err != nil {
		utils.LogAndHTTPError(w, err, "invalid resource URI", http.StatusBadRequest)
		return
	}

	decision := false
	switch req.Action.Name {
	case "can_read":
		var permErr error
		decision, permErr = s.pear.HasPermission(r.Context(), callerDID, requester, owner, collection, rkey)
		if permErr != nil {
			utils.LogAndHTTPError(w, permErr, "permission check failed", http.StatusInternalServerError)
			return
		}
	default:
		log.Warn().Str("action", req.Action.Name).Msg("authzen: unsupported action, denying")
	}

	output := &habitat.NetworkHabitatAuthzenEvaluateOutput{
		Decision: decision,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(output)
}
