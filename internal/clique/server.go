package clique

import (
	"encoding/json"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bradenaw/juniper/xslices"
	"github.com/gorilla/schema"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/utils"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

type Server struct {
	store       Store
	oauth       authn.Method
	serviceAuth authn.Method
	decoder     *schema.Decoder
}

func NewServer(store Store, oauth authn.Method, serviceAuth authn.Method) *Server {
	return &Server{
		store:       store,
		oauth:       oauth,
		serviceAuth: serviceAuth,
		decoder:     schema.NewDecoder(),
	}
}

func (s *Server) CreateClique(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := authn.Validate(w, r, s.oauth)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatCliqueCreateCliqueInput
	err := json.NewDecoder(r.Body).Decode(&input)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode json request", http.StatusBadRequest)
		return
	}

	dids := make([]syntax.DID, len(input.Members))
	for i, m := range input.Members {
		did, err := syntax.ParseDID(m)
		if err != nil {
			utils.LogAndHTTPError(
				r.Context(),
				w,
				err,
				"decode members field of input",
				http.StatusBadRequest,
			)
			return
		}
		dids[i] = did
	}

	clique, err := s.store.CreateClique(r.Context(), callerDID, dids)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"creating clique",
			http.StatusInternalServerError,
		)
		return
	}

	output := habitat.NetworkHabitatCliqueCreateCliqueOutput{
		Clique: string(clique),
	}
	err = json.NewEncoder(w).Encode(output)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "json encoding", http.StatusInternalServerError)
		return
	}
}

func (s *Server) AddCliqueMembers(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := authn.Validate(w, r, s.oauth, s.serviceAuth)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatCliqueAddMembersInput
	err := json.NewDecoder(r.Body).Decode(&input)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode json request", http.StatusBadRequest)
		return
	}

	dids := make([]syntax.DID, len(input.Members))
	for i, m := range input.Members {
		did, err := syntax.ParseDID(m)
		if err != nil {
			utils.LogAndHTTPError(
				r.Context(),
				w,
				err,
				"decode members field of input",
				http.StatusBadRequest,
			)
			return
		}
		dids[i] = did
	}

	clique, err := habitat_syntax.ParseClique(input.Clique.Clique)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode clique", http.StatusBadRequest)
		return
	}

	if callerDID != clique.Authority() {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	err = s.store.AddMembers(r.Context(), clique, dids)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"adding clique members",
			http.StatusInternalServerError,
		)
		return
	}
}

func (s *Server) RemoveCliqueMembers(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := authn.Validate(w, r, s.oauth, s.serviceAuth)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatCliqueRemoveMembersInput
	err := json.NewDecoder(r.Body).Decode(&input)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode json request", http.StatusBadRequest)
		return
	}

	dids := make([]syntax.DID, len(input.Members))
	for i, m := range input.Members {
		did, err := syntax.ParseDID(m)
		if err != nil {
			utils.LogAndHTTPError(
				r.Context(),
				w,
				err,
				"decode members field of input",
				http.StatusBadRequest,
			)
			return
		}
		dids[i] = did
	}

	clique, err := habitat_syntax.ParseClique(input.Clique.Clique)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode clique", http.StatusBadRequest)
		return
	}

	if callerDID != clique.Authority() {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	err = s.store.RemoveMembers(r.Context(), clique, dids)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"removing clique members",
			http.StatusInternalServerError,
		)
		return
	}
}

func (s *Server) GetCliqueMembers(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := authn.Validate(w, r, s.oauth, s.serviceAuth)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatCliqueGetMembersParams
	err := s.decoder.Decode(&params, r.URL.Query())
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode query params", http.StatusBadRequest)
		return
	}

	clique, err := habitat_syntax.ParseClique(params.Clique)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"decode clique from url param",
			http.StatusBadRequest,
		)
		return
	}

	isMember, err := s.store.IsMember(r.Context(), clique, callerDID)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"checking clique membership",
			http.StatusInternalServerError,
		)
		return
	}
	if !isMember {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	dids, err := s.store.GetMembers(r.Context(), clique)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"getting clique members",
			http.StatusInternalServerError,
		)
		return
	}

	members := xslices.Map(dids, func(m syntax.DID) string {
		return m.String()
	})
	output := habitat.NetworkHabitatCliqueGetMembersOutput{
		Members: members,
	}

	err = json.NewEncoder(w).Encode(output)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "json encoding", http.StatusInternalServerError)
		return
	}
}

func (s *Server) IsCliqueMember(w http.ResponseWriter, r *http.Request) {
	callerDID, ok := authn.Validate(w, r, s.oauth, s.serviceAuth)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatCliqueIsMemberParams
	err := s.decoder.Decode(&params, r.URL.Query())
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decode query params", http.StatusBadRequest)
		return
	}

	clique, err := habitat_syntax.ParseClique(params.Clique)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"decode clique from url param",
			http.StatusBadRequest,
		)
		return
	}

	isMember, err := s.store.IsMember(r.Context(), clique, callerDID)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"checking clique membership",
			http.StatusInternalServerError,
		)
		return
	}
	if !isMember {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	did, err := syntax.ParseDID(params.Did)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"decode did from url param",
			http.StatusBadRequest,
		)
		return
	}

	found, err := s.store.IsMember(r.Context(), clique, did)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"checking if clique member",
			http.StatusInternalServerError,
		)
		return
	}

	output := habitat.NetworkHabitatCliqueIsMemberOutput{
		Found: found,
	}
	err = json.NewEncoder(w).Encode(output)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "json encoding", http.StatusInternalServerError)
		return
	}
}
