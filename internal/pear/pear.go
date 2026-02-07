package pear

import (
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/rs/zerolog/log"
)

// pear stands for Permission Enforcing ATProto Repo.
// This package implements that.

// The permissionEnforcingRepo wraps a repo, and enforces permissions on any calls.
type permissionEnforcingRepo struct {
	permissions permissions.Store

	// The backing store for the data. Should implement similar methods to public atproto repos
	repo *repo
}

var (
	ErrPublicRecordExists      = fmt.Errorf("a public record exists with the same key")
	ErrNoPutsOnEncryptedRecord = fmt.Errorf("directly put-ting to this lexicon is not valid")
	ErrNotLocalRepo            = fmt.Errorf("the desired did does not live on this repo")
	ErrUnauthorized            = fmt.Errorf("unauthorized request")
)

func newPermissionEnforcingRepo(perms permissions.Store, repo *repo) *permissionEnforcingRepo {
	return &permissionEnforcingRepo{
		permissions: perms,
		repo:        repo,
	}
}

// putRecord puts the given record on the repo connected to this permissionEnforcingRepo.
// It does not do any encryption, permissions, auth, etc. It is assumed that only the owner of the store can call this and that
// is gated by some higher up level. This should be re-written in the future to not give any incorrect impression.
func (p *permissionEnforcingRepo) putRecord(
	did string,
	collection string,
	record map[string]any,
	rkey string,
	validate *bool,
) error {
	// It is assumed right now that if this endpoint is called, the caller wants to put a private record into pear.
	return p.repo.putRecord(did, collection, rkey, record, validate)
}

// getRecord checks permissions on callerDID and then passes through to `repo.getRecord`.
// It also searches through notifications for records the user has access to, even if they don't belong to the targetDID.
func (p *permissionEnforcingRepo) getRecord(
	collection string,
	rkey string,
	targetDID syntax.DID,
	callerDID syntax.DID,
) (*Record, error) {
	callerDIDStr := callerDID.String()
	targetDIDStr := targetDID.String()

	// Step 1: Try to get the record from targetDID's repo if it exists locally
	has, err := p.hasRepoForDid(targetDIDStr)
	if err != nil {
		return nil, err
	}
	if has {
		// Run permissions before returning to the user
		authz, err := p.permissions.HasPermission(
			callerDIDStr,
			targetDIDStr,
			collection,
			rkey,
		)
		if err != nil {
			return nil, err
		}

		if authz {
			// User has permission, return the record
			return p.repo.getRecord(targetDIDStr, collection, rkey)
		}
		// User doesn't have permission, continue to check notifications
	}

	// Step 2: Search through notifications for records the user has access to
	var joinedResults []struct {
		Notification
		Record
	}

	err = p.repo.db.Table("notifications").
		Select("notifications.*, records.*").
		Joins("INNER JOIN records ON notifications.origin_did = records.did AND notifications.collection = records.collection AND notifications.rkey = records.rkey").
		Where("notifications.did = ?", callerDIDStr).
		Where("notifications.collection = ?", collection).
		Where("notifications.rkey = ?", rkey).
		Find(&joinedResults).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query notifications with records: %w", err)
	}

	// There should only be one notification per record max
	if len(joinedResults) > 1 {
		return nil, fmt.Errorf("multiple notifications found for record %s/%s/%s", collection, rkey, callerDIDStr)
	}

	// Step 3: If we found a notification, check if OriginDid is local and check permissions
	if len(joinedResults) == 1 {
		result := joinedResults[0]
		originDid := result.Notification.OriginDid

		// Check if the record owner is on the same node
		hasLocalRepo, err := p.hasRepoForDid(originDid)
		if err != nil {
			return nil, err
		}

		if !hasLocalRepo {
			// Record is on a different node - log warning and return error
			log.Warn().
				Str("originDid", originDid).
				Str("collection", result.Notification.Collection).
				Str("rkey", result.Notification.Rkey).
				Msg("skipping record from different node (cross-node fetching not yet implemented)")
			return nil, ErrNotLocalRepo
		}

		// Record is on the same node - check permissions
		authz, err := p.permissions.HasPermission(
			callerDIDStr,
			originDid,
			result.Notification.Collection,
			result.Notification.Rkey,
		)
		if err != nil {
			return nil, err
		}

		if !authz {
			// User doesn't have permission for this record
			return nil, ErrUnauthorized
		}

		// Found a record the user has access to - return it
		return &result.Record, nil
	}

	// No record found in targetDID's repo or in notifications
	if has {
		// targetDID exists locally but user doesn't have permission
		return nil, ErrUnauthorized
	}
	// targetDID doesn't exist locally and no matching notifications found
	return nil, ErrNotLocalRepo
}

func (p *permissionEnforcingRepo) listRecords(
	params *habitat.NetworkHabitatRepoListRecordsParams,
	callerDID syntax.DID,
) ([]Record, error) {
	var allRecords []Record
	callerDIDStr := callerDID.String()

	// Step 1: Get records from caller's own repo (if it exists locally)
	hasOwnRepo, err := p.hasRepoForDid(callerDIDStr)
	if err != nil {
		return nil, err
	}
	if !hasOwnRepo {
		return nil, ErrNotLocalRepo
	}
	allow, deny, err := p.permissions.ListReadPermissionsByUser(
		callerDIDStr,
		callerDIDStr,
		params.Collection,
	)
	if err != nil {
		return nil, err
	}

	ownRepoParams := &habitat.NetworkHabitatRepoListRecordsParams{
		Repo:       callerDIDStr,
		Collection: params.Collection,
		Cursor:     params.Cursor,
		Limit:      params.Limit,
		Reverse:    params.Reverse,
	}

	ownRecords, err := p.repo.listRecords(ownRepoParams, allow, deny)
	if err != nil {
		return nil, err
	}
	allRecords = append(allRecords, ownRecords...)

	// Step 2: Query notifications JOINed with records table
	// This automatically filters out notifications without corresponding records
	var joinedResults []struct {
		Notification
		Record
	}

	err = p.repo.db.Table("notifications").
		Select("notifications.*, records.*").
		Joins("INNER JOIN records ON notifications.origin_did = records.did AND notifications.collection = records.collection AND notifications.rkey = records.rkey").
		Where("notifications.did = ?", callerDIDStr).
		Where("notifications.collection = ?", params.Collection).
		Find(&joinedResults).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query notifications with records: %w", err)
	}

	// Step 3: For each joined result, check if OriginDid is local and check permissions
	for _, result := range joinedResults {
		originDid := result.Notification.OriginDid

		// Check if the record owner is on the same node
		hasLocalRepo, err := p.hasRepoForDid(originDid)
		if err != nil {
			return nil, err
		}

		if !hasLocalRepo {
			// Record is on a different node - log warning and skip
			log.Warn().
				Str("originDid", originDid).
				Str("collection", result.Notification.Collection).
				Str("rkey", result.Notification.Rkey).
				Msg("skipping record from different node (cross-node fetching not yet implemented)")
			continue
		}

		// Record is on the same node - check permissions
		authz, err := p.permissions.HasPermission(
			callerDIDStr,
			originDid,
			result.Notification.Collection,
			result.Notification.Rkey,
		)
		if err != nil {
			return nil, err
		}

		if !authz {
			// User doesn't have permission for this record - skip it
			continue
		}

		// Add the record to results
		allRecords = append(allRecords, result.Record)
	}

	return allRecords, nil
}

func (p *permissionEnforcingRepo) hasRepoForDid(did string) (bool, error) {
	has, err := p.repo.hasRepoForDid(did)
	if err != nil {
		return false, err
	}
	return has, nil
}

// TODO: actually enforce permissions here
func (p *permissionEnforcingRepo) getBlob(
	did string,
	cid string,
) (string /* mimetype */, []byte /* raw blob */, error) {
	return p.repo.getBlob(did, cid)
}

// TODO: actually enforce permissions here
func (p *permissionEnforcingRepo) uploadBlob(did string, data []byte, mimeType string) (*blob, error) {
	return p.repo.uploadBlob(did, data, mimeType)
}
