package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/oauthclient"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// ErrForbidden indicates the caller lacks permission to manage a group.
var ErrForbidden = errors.New("not allowed to manage this group")

// ErrInvalidSubject indicates an addMember call did not specify exactly one of
// a user or a group subject.
var ErrInvalidSubject = errors.New("exactly one of subjectDid or subjectGroup must be provided")

// GroupService implements the network.habitat.groups.* endpoints. It reads
// group membership from the sap-fed index (Store) and performs writes against
// pear using the org credential held by the oauth client.
type GroupService struct {
	store    *Store
	oauthApp *oauthclient.App
}

func NewGroupService(store *Store, oauthApp *oauthclient.App) *GroupService {
	return &GroupService{store: store, oauthApp: oauthApp}
}

// orgPear builds a pear client authenticated as the managed org.
func (g *GroupService) orgPear(ctx context.Context) (*pearClient, syntax.DID, error) {
	orgDID, sessionID, err := g.store.OrgSession(ctx)
	if err != nil {
		return nil, "", err
	}
	client, err := g.oauthApp.GetClient(ctx, orgDID, sessionID)
	if err != nil {
		return nil, "", fmt.Errorf("build org client: %w", err)
	}
	return &pearClient{http: client}, orgDID, nil
}

func (g *GroupService) CreateGroup(
	ctx context.Context,
	caller syntax.DID,
	in habitat.NetworkHabitatGroupsCreateGroupInput,
) (habitat.NetworkHabitatGroupsCreateGroupOutput, error) {
	pear, _, err := g.orgPear(ctx)
	if err != nil {
		return habitat.NetworkHabitatGroupsCreateGroupOutput{}, err
	}

	space, err := pear.createGroupSpace(ctx)
	if err != nil {
		return habitat.NetworkHabitatGroupsCreateGroupOutput{}, err
	}

	createdAt := time.Now().UTC().Format(time.RFC3339)
	profileURI, err := pear.putProfile(ctx, space, habitat.NetworkHabitatGroupProfile{
		Name:        in.Name,
		Description: in.Description,
		CreatedAt:   createdAt,
	})
	if err != nil {
		return habitat.NetworkHabitatGroupsCreateGroupOutput{}, err
	}

	// Grant the creator the manager role so they are both a member and able to
	// manage the group.
	tupleURI, err := pear.writeUserTuple(ctx, caller, "manager", space)
	if err != nil {
		return habitat.NetworkHabitatGroupsCreateGroupOutput{}, err
	}

	// Prime the index so the creator sees the new group immediately; sap will
	// reconcile the same rows when the records sync.
	_ = g.store.UpsertProfile(ctx, profileURI, in.Name, in.Description, createdAt)
	_ = g.store.UpsertTuple(ctx, tupleRow{
		RecordURI:   tupleURI.String(),
		ObjectSpace: space.String(),
		Relation:    "manager",
		SubjectKind: "user",
		SubjectDID:  caller.String(),
	})

	return habitat.NetworkHabitatGroupsCreateGroupOutput{Uri: space.String()}, nil
}

func (g *GroupService) UpdateGroup(
	ctx context.Context,
	caller syntax.DID,
	in habitat.NetworkHabitatGroupsUpdateGroupInput,
) (habitat.NetworkHabitatGroupsUpdateGroupOutput, error) {
	space, err := habitat_syntax.ParseSpaceURI(in.Group)
	if err != nil {
		return habitat.NetworkHabitatGroupsUpdateGroupOutput{}, fmt.Errorf(
			"parse group uri: %w",
			err,
		)
	}
	existing, err := g.store.GetGroup(ctx, space)
	if err != nil {
		return habitat.NetworkHabitatGroupsUpdateGroupOutput{}, err
	}

	pear, _, err := g.orgPear(ctx)
	if err != nil {
		return habitat.NetworkHabitatGroupsUpdateGroupOutput{}, err
	}
	if err := g.requireManage(ctx, pear, caller, space); err != nil {
		return habitat.NetworkHabitatGroupsUpdateGroupOutput{}, err
	}

	name := existing.Name
	if in.Name != "" {
		name = in.Name
	}
	description := existing.Description
	if in.Description != "" {
		description = in.Description
	}

	profileURI, err := pear.putProfile(ctx, space, habitat.NetworkHabitatGroupProfile{
		Name:        name,
		Description: description,
		CreatedAt:   existing.CreatedAt,
	})
	if err != nil {
		return habitat.NetworkHabitatGroupsUpdateGroupOutput{}, err
	}
	_ = g.store.UpsertProfile(ctx, profileURI, name, description, existing.CreatedAt)

	return habitat.NetworkHabitatGroupsUpdateGroupOutput{Uri: space.String()}, nil
}

func (g *GroupService) AddMember(
	ctx context.Context,
	caller syntax.DID,
	in habitat.NetworkHabitatGroupsAddMemberInput,
) (habitat.NetworkHabitatGroupsAddMemberOutput, error) {
	if (in.SubjectDid == "") == (in.SubjectGroup == "") {
		return habitat.NetworkHabitatGroupsAddMemberOutput{}, ErrInvalidSubject
	}
	space, err := habitat_syntax.ParseSpaceURI(in.Group)
	if err != nil {
		return habitat.NetworkHabitatGroupsAddMemberOutput{}, fmt.Errorf("parse group uri: %w", err)
	}
	if _, err := g.store.GetGroup(ctx, space); err != nil {
		return habitat.NetworkHabitatGroupsAddMemberOutput{}, err
	}

	pear, _, err := g.orgPear(ctx)
	if err != nil {
		return habitat.NetworkHabitatGroupsAddMemberOutput{}, err
	}
	if err := g.requireManage(ctx, pear, caller, space); err != nil {
		return habitat.NetworkHabitatGroupsAddMemberOutput{}, err
	}

	var tupleURI habitat_syntax.SpaceRecordURI
	var row tupleRow
	if in.SubjectDid != "" {
		did, err := syntax.ParseDID(in.SubjectDid)
		if err != nil {
			return habitat.NetworkHabitatGroupsAddMemberOutput{}, fmt.Errorf(
				"parse subject did: %w",
				err,
			)
		}
		tupleURI, err = pear.writeUserTuple(ctx, did, "writer", space)
		if err != nil {
			return habitat.NetworkHabitatGroupsAddMemberOutput{}, err
		}
		row = tupleRow{
			ObjectSpace: space.String(),
			Relation:    "writer",
			SubjectKind: "user",
			SubjectDID:  did.String(),
		}
	} else {
		subjectGroup, err := habitat_syntax.ParseSpaceURI(in.SubjectGroup)
		if err != nil {
			return habitat.NetworkHabitatGroupsAddMemberOutput{}, fmt.Errorf(
				"parse subject group: %w",
				err,
			)
		}
		tupleURI, err = pear.writeGroupTuple(ctx, subjectGroup, "writer", space)
		if err != nil {
			return habitat.NetworkHabitatGroupsAddMemberOutput{}, err
		}
		row = tupleRow{
			ObjectSpace:  space.String(),
			Relation:     "writer",
			SubjectKind:  "group",
			SubjectGroup: subjectGroup.String(),
			SubjectRole:  "writer",
		}
	}
	row.RecordURI = tupleURI.String()
	_ = g.store.UpsertTuple(ctx, row)

	return habitat.NetworkHabitatGroupsAddMemberOutput{Uri: tupleURI.String()}, nil
}

// requireManage authorizes the caller to manage the space, using pear's
// authoritative FGA check so there is no index lag right after a write.
func (g *GroupService) requireManage(
	ctx context.Context,
	pear *pearClient,
	caller syntax.DID,
	space habitat_syntax.SpaceURI,
) error {
	allowed, err := pear.check(ctx, caller, "manager", space)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

func (g *GroupService) ListGroups(
	ctx context.Context,
	caller syntax.DID,
) (habitat.NetworkHabitatGroupsListGroupsOutput, error) {
	groups, gr, names, err := g.load(ctx)
	if err != nil {
		return habitat.NetworkHabitatGroupsListGroupsOutput{}, err
	}

	out := habitat.NetworkHabitatGroupsListGroupsOutput{
		Groups: []habitat.NetworkHabitatGroupsDefsGroupView{},
	}
	for _, group := range groups {
		// Only surface groups the caller belongs to.
		if !gr.isMember(group.SpaceURI, caller.String()) {
			continue
		}
		out.Groups = append(out.Groups, g.view(group, gr, names, caller, false))
	}
	return out, nil
}

func (g *GroupService) GetGroup(
	ctx context.Context,
	caller syntax.DID,
	groupURI string,
) (habitat.NetworkHabitatGroupsDefsGroupView, error) {
	space, err := habitat_syntax.ParseSpaceURI(groupURI)
	if err != nil {
		return habitat.NetworkHabitatGroupsDefsGroupView{}, fmt.Errorf("parse group uri: %w", err)
	}
	row, err := g.store.GetGroup(ctx, space)
	if err != nil {
		return habitat.NetworkHabitatGroupsDefsGroupView{}, err
	}
	_, gr, names, err := g.load(ctx)
	if err != nil {
		return habitat.NetworkHabitatGroupsDefsGroupView{}, err
	}
	return g.view(row, gr, names, caller, true), nil
}

// load reads all indexed groups and tuples and builds the membership graph and
// a space-uri -> name lookup.
func (g *GroupService) load(ctx context.Context) ([]groupRow, *graph, map[string]string, error) {
	groups, err := g.store.ListGroups(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	tuples, err := g.store.ListTuples(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	names := make(map[string]string, len(groups))
	for _, gr := range groups {
		names[gr.SpaceURI] = gr.Name
	}
	return groups, newGraph(tuples), names, nil
}

// view builds the lexicon group view. When full is true the resolved member
// list is included; the list endpoint omits it for brevity.
func (g *GroupService) view(
	row groupRow,
	gr *graph,
	names map[string]string,
	caller syntax.DID,
	full bool,
) habitat.NetworkHabitatGroupsDefsGroupView {
	members := gr.holders(row.SpaceURI, memberMinRole)

	view := habitat.NetworkHabitatGroupsDefsGroupView{
		Uri:         row.SpaceURI,
		Name:        row.Name,
		Description: row.Description,
		CreatedAt:   row.CreatedAt,
		MemberCount: int64(len(members)),
		IsMember:    gr.isMember(row.SpaceURI, caller.String()),
		CanManage:   gr.canManage(row.SpaceURI, caller.String()),
	}

	for _, inherited := range gr.inheritedGroups(row.SpaceURI) {
		view.InheritedGroups = append(
			view.InheritedGroups,
			habitat.NetworkHabitatGroupsDefsGroupRef{
				Uri:  inherited,
				Name: names[inherited],
			},
		)
	}

	if full {
		sort.Slice(members, func(i, j int) bool { return members[i].DID < members[j].DID })
		for _, m := range members {
			view.Members = append(view.Members, habitat.NetworkHabitatGroupsDefsMemberView{
				Did:      m.DID,
				Role:     m.Role,
				Direct:   m.Direct,
				ViaGroup: m.ViaGroup,
			})
		}
	}
	return view
}
