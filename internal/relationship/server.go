package relationship

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/gorilla/schema"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/perms"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Server exposes the network.habitat.relationship.* XRPC endpoints. It is a
// thin XRPC layer over perms.Store, which owns storage and permission logic.
// Writes require the manager role and reads require the reader role on the
// governing space.
type Server struct {
	orgStore  org.Store
	perms     perms.Store
	spaces    spaces.Store
	validator authn.RequestValidator
	decoder   *schema.Decoder
}

func NewServer(
	permsStore perms.Store,
	spacesStore spaces.Store,
	validator authn.RequestValidator,
) *Server {
	return &Server{
		perms:     permsStore,
		spaces:    spacesStore,
		validator: validator,
		decoder:   schema.NewDecoder(),
	}
}

func (s *Server) WriteUserRelation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatRelationshipWriteUserRelationInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to decode request body", err)
		return
	}
	subject, ok := httpx.ParseDIDInput(ctx, w, input.Subject, "subject")
	if !ok {
		return
	}
	space, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space")
	if !ok {
		return
	}
	if _, ok = s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(space, habitat_syntax.SpaceRoleManager),
	).Validate(w, r); !ok {
		return
	}
	role, err := parseSpaceRole(input.Relation)
	if err != nil {
		httpx.WriteError(ctx, w, "InvalidRelation", err.Error(), http.StatusBadRequest)
		return
	}
	uri, err := s.perms.SetUserRelation(ctx, subject, space, role)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("add user relation: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w,
		habitat.NetworkHabitatRelationshipWriteUserRelationOutput{Uri: uri.String()})
}

func (s *Server) WriteSpaceRelation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatRelationshipWriteSpaceRelationInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to decode request body", err)
		return
	}
	subject, ok := httpx.ParseSpaceURIInput(ctx, w, input.Subject, "subject")
	if !ok {
		return
	}
	space, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space")
	if !ok {
		return
	}
	if _, ok = s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(space, fgastore.RelationSpaceMemberManager),
	).Validate(w, r); !ok {
		return
	}
	subjectRole, err := parseSpaceRole(input.SubjectRole)
	if err != nil {
		httpx.WriteError(ctx, w, "InvalidRelation", err.Error(), http.StatusBadRequest)
		return
	}
	relation, err := parseSpaceRole(input.Relation)
	if err != nil {
		httpx.WriteError(ctx, w, "InvalidRelation", err.Error(), http.StatusBadRequest)
		return
	}
	uri, err := s.perms.SetSpaceRoleRelation(ctx, subject, subjectRole, space, relation)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("add space role relation: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w,
		habitat.NetworkHabitatRelationshipWriteSpaceRelationOutput{Uri: uri.String()})
}

func (s *Server) DeleteRelation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatRelationshipDeleteRelationInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to decode request body", err)
		return
	}
	uri, err := habitat_syntax.ParseSpaceRecordURI(input.Uri)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse uri", err)
		return
	}
	if _, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(uri.SpaceURI(), habitat_syntax.SpaceRoleManager),
	).Validate(w, r); !ok {
		return
	}
	err = s.perms.DeleteRelation(ctx, uri)
	if errors.Is(err, perms.ErrRelationNotFound) {
		slog.WarnContext(ctx, "relation not found", "err", err)
		httpx.WriteError(ctx, w, "RelationNotFound", "", http.StatusNotFound)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("delete relation: %w", err))
		return
	}
}

func (s *Server) CheckUserRelation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params habitat.NetworkHabitatRelationshipCheckUserRelationParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to decode query params", err)
		return
	}
	subject, ok := httpx.ParseDIDInput(ctx, w, params.Subject, "subject")
	if !ok {
		return
	}
	space, ok := httpx.ParseSpaceURIInput(ctx, w, params.Space, "space")
	if !ok {
		return
	}
	if _, ok = s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(space, habitat_syntax.SpaceRoleReader),
	).Validate(w, r); !ok {
		return
	}
	role, err := parseSpaceRole(params.Relation)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse relation", err)
		return
	}
	allowed, err := s.perms.CheckUserHasSpaceRole(ctx, subject, space, role)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("check user relation: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w,
		habitat.NetworkHabitatRelationshipCheckUserRelationOutput{Allowed: allowed})
}

func (s *Server) CheckSpaceRelation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params habitat.NetworkHabitatRelationshipCheckSpaceRelationParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to decode query params", err)
		return
	}
	subject, ok := httpx.ParseSpaceURIInput(ctx, w, params.Subject, "subject")
	if !ok {
		return
	}
	space, ok := httpx.ParseSpaceURIInput(ctx, w, params.Space, "space")
	if !ok {
		return
	}
	if _, ok = s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(space, habitat_syntax.SpaceRoleReader),
	).Validate(w, r); !ok {
		return
	}
	subjectRole, err := parseSpaceRole(params.SubjectRole)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse subjectRole", err)
		return
	}
	relation, err := parseSpaceRole(params.Relation)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse relation", err)
		return
	}
	allowed, err := s.perms.CheckSpaceRelationHasSpaceRole(
		ctx,
		subject,
		subjectRole,
		space,
		relation,
	)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("check space relation: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w,
		habitat.NetworkHabitatRelationshipCheckSpaceRelationOutput{Allowed: allowed})
}

func (s *Server) ListSubjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params habitat.NetworkHabitatRelationshipListSubjectsParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to decode query params", err)
		return
	}
	space, ok := httpx.ParseSpaceURIInput(ctx, w, params.Space, "space")
	if !ok {
		return
	}
	if _, ok = s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(space, habitat_syntax.SpaceRoleReader),
	).Validate(w, r); !ok {
		return
	}
	role, err := parseSpaceRole(params.Relation)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse relation", err)
		return
	}
	dids, err := s.perms.ListUserSubjects(ctx, space, role)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list subjects: %w", err))
		return
	}
	out := make([]string, len(dids))
	for i, did := range dids {
		out[i] = did.String()
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatRelationshipListSubjectsOutput{Dids: out})
}

func (s *Server) ListObjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var params habitat.NetworkHabitatRelationshipListObjectsParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to decode query params", err)
		return
	}
	did, ok := httpx.ParseDIDInput(ctx, w, params.Did, "did")
	if !ok {
		return
	}
	role, err := parseSpaceRole(params.Relation)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse relation", err)
		return
	}
	var filterType *syntax.NSID
	if params.Type != "" {
		t, ok := httpx.ParseNSIDInput(ctx, w, params.Type, "type filter")
		if !ok {
			return
		}
		filterType = &t
	}
	spaceURIs, err := s.perms.ListObjects(ctx, did, role, filterType)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list objects: %w", err))
		return
	}
	// Only return spaces the caller is allowed to read.
	out := make([]string, 0, len(spaceURIs))
	for _, space := range spaceURIs {
		readable, err := s.perms.CheckUserHasSpaceRole(
			ctx,
			credInfo.Subject,
			space,
			habitat_syntax.SpaceRoleReader,
		)
		if err != nil {
			httpx.WriteServerError(ctx, w, fmt.Errorf("check read permission: %w", err))
			return
		}
		if readable {
			out = append(out, space.String())
		}
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatRelationshipListObjectsOutput{Spaces: out})
}

func (s *Server) ListRelations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params habitat.NetworkHabitatRelationshipListRelationsParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to decode query params", err)
		return
	}
	space, ok := httpx.ParseSpaceURIInput(ctx, w, params.Space, "space uri")
	if !ok {
		return
	}
	if _, ok = s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(space, fgastore.RelationSpaceReader),
	).Validate(w, r); !ok {
		return
	}
	if params.SubjectType != "" && params.SubjectType != "user" && params.SubjectType != "space" {
		httpx.WriteInvalidRequest(ctx, w, "invalid subjectType", nil)
		return
	}
	if params.Object != "" && params.Object != space.String() {
		// The object of a relation record is always the space it's written
		// into (the governing space itself), so a mismatched object filter
		// can never match anything.
		httpx.WriteJSON(ctx, w, habitat.NetworkHabitatRelationshipListRelationsOutput{
			Relations: []any{},
		})
		return
	}

	views := make([]any, 0)

	if params.SubjectType != "space" {
		userViews, err := s.listUserRelationViews(ctx, space, params)
		if err != nil {
			httpx.WriteServerError(ctx, w, fmt.Errorf("list user relations: %w", err))
			return
		}
		views = append(views, userViews...)
	}
	if params.SubjectType != "user" {
		spaceViews, err := s.listSpaceRelationViews(ctx, space, params)
		if err != nil {
			httpx.WriteServerError(ctx, w, fmt.Errorf("list space relations: %w", err))
			return
		}
		views = append(views, spaceViews...)
	}

	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatRelationshipListRelationsOutput{Relations: views})
}

// listUserRelationViews reads and filters the userRelation records governing
// space.
func (s *Server) listUserRelationViews(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	params habitat.NetworkHabitatRelationshipListRelationsParams,
) ([]any, error) {
	collection := habitat_syntax.UserRelationCollection
	records, err := s.spaces.ListRecords(ctx, space, space.SpaceOwner(), &collection)
	if err != nil {
		return nil, err
	}
	views := make([]any, 0, len(records))
	for _, rec := range records {
		subjectDID, _ := rec.Value["subject"].(string)
		relation, _ := rec.Value["relation"].(string)
		if params.SubjectDid != "" && subjectDID != params.SubjectDid {
			continue
		}
		if params.Relation != "" && relation != params.Relation {
			continue
		}
		views = append(views, habitat.NetworkHabitatRelationshipListRelationsUserRelationView{
			Uri: habitat_syntax.ConstructSpaceRecordURI(
				space, rec.Owner, rec.Collection, rec.Rkey,
			).String(),
			Subject:  subjectDID,
			Relation: relation,
			Object:   space.String(),
		})
	}
	return views, nil
}

// listSpaceRelationViews reads and filters the spaceRelation records
// governing space.
func (s *Server) listSpaceRelationViews(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	params habitat.NetworkHabitatRelationshipListRelationsParams,
) ([]any, error) {
	collection := habitat_syntax.SpaceRelationCollection
	records, err := s.spaces.ListRecords(ctx, space, space.SpaceOwner(), &collection)
	if err != nil {
		return nil, err
	}
	views := make([]any, 0, len(records))
	for _, rec := range records {
		subject, _ := rec.Value["subject"].(string)
		subjectRole, _ := rec.Value["subjectRole"].(string)
		relation, _ := rec.Value["relation"].(string)
		// subjectDid only filters userRelation records; a spaceRelation's
		// subject is a space, never a DID.
		if params.SubjectDid != "" {
			continue
		}
		if params.Relation != "" && relation != params.Relation {
			continue
		}
		views = append(views, habitat.NetworkHabitatRelationshipListRelationsSpaceRelationView{
			Uri: habitat_syntax.ConstructSpaceRecordURI(
				space, rec.Owner, rec.Collection, rec.Rkey,
			).String(),
			Subject:     subject,
			SubjectRole: subjectRole,
			Relation:    relation,
			Object:      space.String(),
		})
	}
	return views, nil
}
