package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// GroupSpaceType is the space type every group is created under. The
// network.habitat.group.profile self record holds the group's metadata.
const GroupSpaceType = "network.habitat.group"

const (
	collectionGroupProfile = "network.habitat.group.profile"
	collectionTuple        = "network.habitat.relationship.tuple"
)

// pearClient wraps the network.habitat.* XRPC endpoints, calling them through
// an org-credentialed http.Client (produced by oauth_client). The client
// rewrites relative paths onto the org's pear host, so requests use "/xrpc/..."
// paths.
type pearClient struct {
	http *http.Client
}

func (p *pearClient) post(ctx context.Context, nsid string, input any, out any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("marshal %s input: %w", nsid, err)
	}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, "/xrpc/"+nsid, bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("build %s request: %w", nsid, err)
	}
	req.Header.Set("Content-Type", "application/json")
	return p.do(req, nsid, out)
}

func (p *pearClient) get(ctx context.Context, nsid string, params url.Values, out any) error {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, "/xrpc/"+nsid+"?"+params.Encode(), nil,
	)
	if err != nil {
		return fmt.Errorf("build %s request: %w", nsid, err)
	}
	return p.do(req, nsid, out)
}

func (p *pearClient) do(req *http.Request, nsid string, out any) error {
	resp, err := p.http.Do(req)
	if err != nil {
		return fmt.Errorf("call %s: %w", nsid, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s returned %d: %s", nsid, resp.StatusCode, string(msg))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s response: %w", nsid, err)
	}
	return nil
}

// createGroupSpace creates a new network.habitat.group space owned by the org.
func (p *pearClient) createGroupSpace(ctx context.Context) (habitat_syntax.SpaceURI, error) {
	var out habitat.NetworkHabitatSpaceCreateSpaceOutput
	err := p.post(ctx, "network.habitat.space.createSpace",
		habitat.NetworkHabitatSpaceCreateSpaceInput{Type: GroupSpaceType}, &out)
	if err != nil {
		return "", err
	}
	return habitat_syntax.SpaceURI(out.Uri), nil
}

// putProfile writes (or overwrites) a group-space's profile self record.
func (p *pearClient) putProfile(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	profile habitat.NetworkHabitatGroupProfile,
) (habitat_syntax.SpaceRecordURI, error) {
	var out habitat.NetworkHabitatSpacePutRecordOutput
	err := p.post(ctx, "network.habitat.space.putRecord",
		habitat.NetworkHabitatSpacePutRecordInput{
			Space:      space.String(),
			Collection: collectionGroupProfile,
			Rkey:       "self",
			Record:     profile,
		}, &out)
	if err != nil {
		return "", err
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
	err := p.post(ctx, "network.habitat.relationship.writeTuple",
		habitat.NetworkHabitatRelationshipWriteTupleInput{
			Subject:  subject,
			Relation: relation,
			Object:   habitat.NetworkHabitatRelationshipDefsSpaceObject{Space: object.String()},
		}, &out)
	if err != nil {
		return "", err
	}
	return habitat_syntax.SpaceRecordURI(out.Uri), nil
}

// deleteTuple removes a relationship tuple by its record URI.
func (p *pearClient) deleteTuple(ctx context.Context, uri habitat_syntax.SpaceRecordURI) error {
	return p.post(ctx, "network.habitat.relationship.deleteTuple",
		habitat.NetworkHabitatRelationshipDeleteTupleInput{Uri: uri.String()}, nil)
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
	err := p.get(ctx, "network.habitat.relationship.listObjects", url.Values{
		"did":      []string{did.String()},
		"relation": []string{relation},
	}, &out)
	if err != nil {
		return nil, err
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
	err := p.get(ctx, "network.habitat.relationship.check", url.Values{
		"subject":  []string{did.String()},
		"relation": []string{relation},
		"space":    []string{space.String()},
	}, &out)
	if err != nil {
		return false, err
	}
	return out.Allowed, nil
}
