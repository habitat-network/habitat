package pearserver

import (
	"context"
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// parseSpaceRole validates a role string against the roles known to the
// lexicon (owner|manager|writer|reader) and converts it to
// habitat_syntax.SpaceRole.
func parseSpaceRole(role string) (habitat_syntax.SpaceRole, error) {
	switch habitat_syntax.SpaceRole(role) {
	case habitat_syntax.SpaceRoleOwner,
		habitat_syntax.SpaceRoleManager,
		habitat_syntax.SpaceRoleWriter,
		habitat_syntax.SpaceRoleReader:
		return habitat_syntax.SpaceRole(role), nil
	default:
		return "", fmt.Errorf("invalid role: %s", role)
	}
}

// authorizeCanWrite returns whether the caller may grant role to the subject
// on space, writing an error response and returning false if not. Owners may
// grant any role; non-owners may not grant ownership and may not alter an
// owner's current role.
func (p *PearServer) authorizeCanWrite(
	ctx context.Context,
	w http.ResponseWriter,
	credInfo *authn.CredentialInfo,
	isSubjectCurrentlyOwner bool,
	space habitat_syntax.SpaceURI,
	role habitat_syntax.SpaceRole,
) bool {
	isCallerOwner, err := p.permStore.CheckUserHasSpaceRole(
		ctx,
		credInfo.Subject,
		space,
		habitat_syntax.SpaceRoleOwner,
	)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("check owner: %w", err))
		return false
	}
	if isCallerOwner {
		return true
	}
	if role == habitat_syntax.SpaceRoleOwner {
		httpx.WriteInvalidRequest(ctx, w, "caller must be owner to grant ownership to others", nil)
		return false
	}
	if isSubjectCurrentlyOwner {
		httpx.WriteInvalidRequest(ctx, w, "caller must be owner to change other owners", nil)
		return false
	}
	return true
}

// listUserRelationViews reads and filters the userRelation records governing
// space.
func (p *PearServer) listUserRelationViews(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	params habitat.NetworkHabitatRelationshipListRelationsParams,
) ([]any, error) {
	records, err := p.spacesStore.ListRecords(
		ctx,
		space,
		space.SpaceOwner(),
		new(habitat_syntax.UserRelationCollection),
	)
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
func (p *PearServer) listSpaceRelationViews(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	params habitat.NetworkHabitatRelationshipListRelationsParams,
) ([]any, error) {
	records, err := p.spacesStore.ListRecords(
		ctx,
		space,
		space.SpaceOwner(),
		new(habitat_syntax.SpaceRelationCollection),
	)
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
