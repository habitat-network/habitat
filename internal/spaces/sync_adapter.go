package spaces

import (
	"context"
	"encoding/json"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/sync"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

type syncStoreAdapter struct {
	store Store
}

func NewSyncStoreAdapter(store Store) sync.SpaceStore {
	return &syncStoreAdapter{store: store}
}

func (a *syncStoreAdapter) ListSpaces(
	ctx context.Context,
	member syntax.DID,
	filterOwner *syntax.DID,
	filterType *syntax.NSID,
) ([]sync.SpaceView, error) {
	views, err := a.store.ListSpaces(ctx, member, filterOwner, filterType)
	if err != nil {
		return nil, err
	}
	result := make([]sync.SpaceView, len(views))
	for i, v := range views {
		result[i] = sync.SpaceView{
			Space: v.URI.String(),
			Type:  v.Type.String(),
			Skey:  v.Skey.String(),
		}
	}
	return result, nil
}

func (a *syncStoreAdapter) GetSpaceState(ctx context.Context, space string) (*sync.SpaceState, error) {
	uri, err := habitat_syntax.ParseSpaceURI(space)
	if err != nil {
		return nil, err
	}
	state, err := a.store.GetSpaceState(ctx, uri)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, nil
	}
	repos := make([]sync.SpaceRepoState, len(state.Repos))
	for i, r := range state.Repos {
		repos[i] = sync.SpaceRepoState{DID: r.DID, Rev: r.Rev}
	}
	return &sync.SpaceState{
		Space:     state.Space,
		SpaceType: state.SpaceType,
		SpaceRev:  state.SpaceRev,
		MemberRev: state.MemberRev,
		Repos:     repos,
	}, nil
}

func (a *syncStoreAdapter) ListRecordChanges(
	ctx context.Context,
	space string,
	repo string,
	since string,
	limit int,
) ([]sync.RecordChange, error) {
	uri, err := habitat_syntax.ParseSpaceURI(space)
	if err != nil {
		return nil, err
	}
	changes, err := a.store.ListRecordChanges(ctx, uri, repo, since, limit)
	if err != nil {
		return nil, err
	}
	result := make([]sync.RecordChange, len(changes))
	for i, c := range changes {
		rc := sync.RecordChange{
			Space:      c.Space,
			Rev:        c.Rev,
			Action:     c.Action,
			Collection: c.Collection,
			Rkey:       c.Rkey,
		}
		if c.Value != nil {
			rc.Value = c.Value
		}
		result[i] = rc
	}
	return result, nil
}

func (a *syncStoreAdapter) GetMemberOplog(
	ctx context.Context,
	space string,
	since string,
	limit int,
) ([]sync.MemberOp, error) {
	uri, err := habitat_syntax.ParseSpaceURI(space)
	if err != nil {
		return nil, err
	}
	ops, err := a.store.GetMemberOplog(ctx, uri, since, limit)
	if err != nil {
		return nil, err
	}
	result := make([]sync.MemberOp, len(ops))
	for i, o := range ops {
		mop := sync.MemberOp{
			Space:  o.Space,
			Rev:    o.Rev,
			Idx:    o.Idx,
			Action: o.Action,
			DID:    o.DID,
		}
		if o.Access != nil {
			mop.Access = o.Access
		}
		result[i] = mop
	}
	return result, nil
}

func (a *syncStoreAdapter) IsMember(ctx context.Context, space string, did string) (bool, error) {
	uri, err := habitat_syntax.ParseSpaceURI(space)
	if err != nil {
		return false, err
	}
	memberDID, err := syntax.ParseDID(did)
	if err != nil {
		return false, err
	}
	return a.store.IsMember(ctx, uri, memberDID)
}

func (a *syncStoreAdapter) GetSpace(ctx context.Context, space string) (*sync.SpaceView, error) {
	uri, err := habitat_syntax.ParseSpaceURI(space)
	if err != nil {
		return nil, err
	}
	state, err := a.store.GetSpaceState(ctx, uri)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, nil
	}
	return &sync.SpaceView{
		Space:     state.Space,
		Type:      state.SpaceType,
		SpaceRev:  state.SpaceRev,
		MemberRev: state.MemberRev,
	}, nil
}

func (a *syncStoreAdapter) GetEvents(ctx context.Context, since int64, limit int) ([]sync.Event, error) {
	rows, err := a.store.GetEvents(ctx, since, limit)
	if err != nil {
		return nil, err
	}
	result := make([]sync.Event, len(rows))
	for i, r := range rows {
		var ev sync.Event
		if err := json.Unmarshal(r.EventJSON, &ev); err != nil {
			return nil, err
		}
		ev.Seq = r.Seq
		result[i] = ev
	}
	return result, nil
}
