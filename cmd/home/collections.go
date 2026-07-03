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
// records in each (counted once per space a record belongs to).
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

// ListRecords lists the records in a collection the caller can see. Each record
// is scoped to a single space: the same repo/collection/rkey in a different
// space is a distinct record (each space holds its own version), so it appears
// as its own entry.
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
		Records: recordViews(rows),
	}, nil
}

// recordViews maps each per-space record row to its own view, keyed by the
// space-record URI.
func recordViews(rows []recordRow) []habitat.NetworkHabitatCollectionsDefsRecordView {
	views := make([]habitat.NetworkHabitatCollectionsDefsRecordView, 0, len(rows))
	for _, row := range rows {
		views = append(views, habitat.NetworkHabitatCollectionsDefsRecordView{
			Uri:        row.RecordURI,
			Space:      row.SpaceURI,
			Repo:       row.Repo,
			Collection: row.Collection,
			Rkey:       row.Rkey,
		})
	}
	return views
}
