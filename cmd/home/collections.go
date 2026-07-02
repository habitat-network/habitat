package main

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/oauthclient"
)

// CollectionService implements the network.habitat.collections.* endpoints. It
// reads the sap-fed record index (Store) and lets a user browse the org's
// synced data by collection type. Permissions are enforced by asking pear
// (as the org) which spaces the caller can read and only surfacing records in
// those spaces.
type CollectionService struct {
	store    *Store
	oauthApp *oauthclient.App
}

func NewCollectionService(store *Store, oauthApp *oauthclient.App) *CollectionService {
	return &CollectionService{store: store, oauthApp: oauthApp}
}

// readableSpaces returns the spaces the caller is allowed to read, resolved
// authoritatively by pear's FGA. Records outside this set are never surfaced.
func (c *CollectionService) readableSpaces(
	ctx context.Context,
	caller syntax.DID,
) ([]string, error) {
	orgDID, sessionID, err := c.store.OrgSession(ctx)
	if err != nil {
		return nil, err
	}
	client, err := c.oauthApp.GetClient(ctx, orgDID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("build org client: %w", err)
	}
	pear := &pearClient{http: client}
	return pear.listObjects(ctx, caller, "reader")
}

// ListCollections lists the collections the caller can see with a count of the
// distinct records in each.
func (c *CollectionService) ListCollections(
	ctx context.Context,
	caller syntax.DID,
) (habitat.NetworkHabitatCollectionsListCollectionsOutput, error) {
	spaces, err := c.readableSpaces(ctx, caller)
	if err != nil {
		return habitat.NetworkHabitatCollectionsListCollectionsOutput{}, err
	}
	counts, err := c.store.CountCollections(ctx, spaces)
	if err != nil {
		return habitat.NetworkHabitatCollectionsListCollectionsOutput{}, err
	}
	out := habitat.NetworkHabitatCollectionsListCollectionsOutput{
		Collections: []habitat.NetworkHabitatCollectionsDefsCollectionView{},
	}
	for _, cc := range counts {
		out.Collections = append(
			out.Collections,
			habitat.NetworkHabitatCollectionsDefsCollectionView{
				Collection:  cc.Collection,
				RecordCount: cc.Count,
			},
		)
	}
	return out, nil
}

// ListRecords lists the records in a collection the caller can see, collapsing
// the per-space copies of the same atproto record into one entry that lists
// every space (the caller can read) the record belongs to.
func (c *CollectionService) ListRecords(
	ctx context.Context,
	caller syntax.DID,
	collection string,
) (habitat.NetworkHabitatCollectionsListRecordsOutput, error) {
	spaces, err := c.readableSpaces(ctx, caller)
	if err != nil {
		return habitat.NetworkHabitatCollectionsListRecordsOutput{}, err
	}
	rows, err := c.store.ListRecordsInSpaces(ctx, spaces, collection)
	if err != nil {
		return habitat.NetworkHabitatCollectionsListRecordsOutput{}, err
	}
	return habitat.NetworkHabitatCollectionsListRecordsOutput{
		Records: groupRecordViews(rows),
	}, nil
}

// groupRecordViews collapses per-space record rows into one view per atproto
// record, listing every space the record belongs to. First-seen order is
// preserved (rows arrive ordered by at_uri); positions are tracked by index so
// appends that reallocate the backing array stay consistent.
func groupRecordViews(rows []recordRow) []habitat.NetworkHabitatCollectionsDefsRecordView {
	indexByRecord := map[string]int{}
	views := []habitat.NetworkHabitatCollectionsDefsRecordView{}
	for _, row := range rows {
		i, ok := indexByRecord[row.AtURI]
		if !ok {
			views = append(views, habitat.NetworkHabitatCollectionsDefsRecordView{
				Uri:        row.AtURI,
				Repo:       row.Repo,
				Collection: row.Collection,
				Rkey:       row.Rkey,
				Spaces:     []string{},
			})
			i = len(views) - 1
			indexByRecord[row.AtURI] = i
		}
		views[i].Spaces = append(views[i].Spaces, row.SpaceURI)
	}
	return views
}
