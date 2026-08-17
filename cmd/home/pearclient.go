package main

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/pkg/oauthclient"
)

// GroupSpaceType is the space type every group is created under. The
// network.habitat.group.profile self record holds the group's metadata.
const GroupSpaceType = "network.habitat.group"

const (
	collectionGroupProfile = "network.habitat.group.profile"
	collectionTuple        = "network.habitat.relationship.tuple"
)

// pearClient wraps the network.habitat.* XRPC endpoints, calling them through
// an org-credentialed atclient.APIClient (produced by
// oauth.ClientSession.APIClient, which resolves requests against the org's
// PDS host and handles DPoP/refresh via the session it wraps).
type pearClient struct {
	*atclient.APIClient
}

// resumeOrgSession builds a pear client authenticated as orgDID. orgDID must
// already be registered (via network.habitat.org.requestCrawl); callers that
// have a target space should derive it from habitat_syntax.SpaceURI.SpaceOwner
// rather than guessing, since home can be registered for several orgs at
// once and each org's credential can only see its own spaces.
//
// A session bootstrapped via the JWT Bearer grant (see main.go/OrgService)
// carries no refresh token (RFC 7523 issues an access token only), so once it
// expires oauth.ClientSession's normal refresh_token flow can never renew it —
// every call fails with invalid_grant until the process restarts. Detect that
// case and mint a fresh access token via a new JWT Bearer grant instead of
// resuming the stale one; a session from the browser /oauth/login flow does
// carry a refresh token and takes the normal (cheaper) resume path.
func resumeOrgSession(
	ctx context.Context,
	oauthApp *oauth.ClientApp,
	store *Store,
	orgDID syntax.DID,
) (*pearClient, error) {
	sessionID, err := store.OrgSessionID(ctx, orgDID)
	if err != nil {
		return nil, err
	}
	session, err := oauthApp.ResumeSession(ctx, orgDID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("build org client: %w", err)
	}
	if session.Data.RefreshToken == "" {
		sess, err := oauthclient.NewJWTBearerClient(oauthApp).
			SendJWTTokenRequest(ctx, orgDID.String())
		if err != nil {
			return nil, fmt.Errorf("remint org session: %w", err)
		}
		if err := store.SaveOrgSession(ctx, sess.AccountDID, ""); err != nil {
			return nil, fmt.Errorf("save reminted org session: %w", err)
		}
		session, err = oauthApp.ResumeSession(ctx, sess.AccountDID, "")
		if err != nil {
			return nil, fmt.Errorf("resume reminted org session: %w", err)
		}
	}
	return &pearClient{APIClient: session.APIClient()}, nil
}

// createGroupSpace creates a new network.habitat.group space owned by the org.
func (p *pearClient) createGroupSpace(ctx context.Context) (habitat_syntax.SpaceURI, error) {
	var out habitat.NetworkHabitatSimplespaceCreateSpaceOutput
	err := p.Post(ctx, "network.habitat.simplespace.createSpace",
		habitat.NetworkHabitatSimplespaceCreateSpaceInput{Type: GroupSpaceType}, &out)
	if err != nil {
		return "", fmt.Errorf("call network.habitat.simplespace.createSpace: %w", err)
	}
	return habitat_syntax.SpaceURI(out.Uri), nil
}

// putProfile writes (or overwrites) a group-space's profile self record.
// repo must be the org's own DID (the org credential's subject); pear rejects
// writes where it differs.
func (p *pearClient) putProfile(
	ctx context.Context,
	repo syntax.DID,
	space habitat_syntax.SpaceURI,
	profile habitat.NetworkHabitatGroupProfile,
) (habitat_syntax.SpaceRecordURI, error) {
	var out habitat.NetworkHabitatSpacePutRecordOutput
	err := p.Post(ctx, "network.habitat.space.putRecord",
		habitat.NetworkHabitatSpacePutRecordInput{
			Space:      space.String(),
			Repo:       repo.String(),
			Collection: collectionGroupProfile,
			Rkey:       "self",
			Record:     profile,
		}, &out)
	if err != nil {
		return "", fmt.Errorf("call network.habitat.space.putRecord: %w", err)
	}
	return habitat_syntax.SpaceRecordURI(out.Uri), nil
}

// writeUserTuple grants a user a role on a group-space.
func (p *pearClient) writeUserTuple(
	ctx context.Context,
	did syntax.DID,
	relation string,
	object habitat_syntax.SpaceURI,
) (habitat_syntax.SpaceRecordURI, error) {
	return p.writeTuple(ctx, map[string]any{
		"$type": "network.habitat.relationship.defs#userSubject",
		"did":   did.String(),
	}, relation, object)
}

// writeGroupTuple grants the writers of subjectGroup a role on a group-space,
// i.e. makes object inherit subjectGroup's members.
func (p *pearClient) writeGroupTuple(
	ctx context.Context,
	subjectGroup habitat_syntax.SpaceURI,
	relation string,
	object habitat_syntax.SpaceURI,
) (habitat_syntax.SpaceRecordURI, error) {
	return p.writeTuple(ctx, map[string]any{
		"$type": "network.habitat.relationship.defs#spaceRoleSubject",
		"space": subjectGroup.String(),
		"role":  "writer",
	}, relation, object)
}

func (p *pearClient) writeTuple(
	ctx context.Context,
	subject map[string]any,
	relation string,
	object habitat_syntax.SpaceURI,
) (habitat_syntax.SpaceRecordURI, error) {
	var out habitat.NetworkHabitatRelationshipWriteTupleOutput
	err := p.Post(ctx, "network.habitat.relationship.writeTuple",
		habitat.NetworkHabitatRelationshipWriteTupleInput{
			Subject:  subject,
			Relation: relation,
			Object:   habitat.NetworkHabitatRelationshipDefsSpaceObject{Space: object.String()},
		}, &out)
	if err != nil {
		return "", fmt.Errorf("call network.habitat.relationship.writeTuple: %w", err)
	}
	return habitat_syntax.SpaceRecordURI(out.Uri), nil
}

// deleteTuple removes a relationship tuple by its record URI.
func (p *pearClient) deleteTuple(ctx context.Context, uri habitat_syntax.SpaceRecordURI) error {
	err := p.Post(ctx, "network.habitat.relationship.deleteTuple",
		habitat.NetworkHabitatRelationshipDeleteTupleInput{Uri: uri.String()}, nil)
	if err != nil {
		return fmt.Errorf("call network.habitat.relationship.deleteTuple: %w", err)
	}
	return nil
}

// listObjects returns the spaces on which did holds relation, resolved
// authoritatively by pear's FGA. pear only returns spaces the calling
// credential (the org) can also read, so this is the org-visible set of spaces
// the user can see.
func (p *pearClient) listObjects(
	ctx context.Context,
	did syntax.DID,
	relation string,
) ([]string, error) {
	var out habitat.NetworkHabitatRelationshipListObjectsOutput
	err := p.Get(ctx, "network.habitat.relationship.listObjects", map[string]any{
		"did":      did.String(),
		"relation": relation,
	}, &out)
	if err != nil {
		return nil, fmt.Errorf("call network.habitat.relationship.listObjects: %w", err)
	}
	return out.Spaces, nil
}

// check reports whether did holds relation on space, resolved authoritatively
// by pear's FGA (expanding usersets and role implications) with no index lag.
func (p *pearClient) check(
	ctx context.Context,
	did syntax.DID,
	relation string,
	space habitat_syntax.SpaceURI,
) (bool, error) {
	var out habitat.NetworkHabitatRelationshipCheckOutput
	err := p.Get(ctx, "network.habitat.relationship.check", map[string]any{
		"subject":  did.String(),
		"relation": relation,
		"space":    space.String(),
	}, &out)
	if err != nil {
		return false, fmt.Errorf("call network.habitat.relationship.check: %w", err)
	}
	return out.Allowed, nil
}
