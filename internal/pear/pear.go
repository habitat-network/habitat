package pear

import (
	"context"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/inbox"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/rs/zerolog/log"
)

// pear stands for Permission Enforcing ATProto Repo.
// This package implements that.

// The permissionEnforcingRepo wraps a repo, and enforces permissions on any calls.
type Pear struct {
	ctx context.Context
	// The URL at which this repo lives; should match what is in a hosted user's DID doc for the habitat service entry
	url string
	// The service name for habitat in the DID doc (different for dev / production)
	serviceName string
	dir         identity.Directory

	// Backing for permissions
	permissions permissions.Store

	// The backing store for the data. Should implement similar methods to public atproto repos
	repo *repo

	// Manage receiving updates for records (replacement for the Firehose)
	inbox inbox.Inbox
}

var (
	ErrPublicRecordExists      = fmt.Errorf("a public record exists with the same key")
	ErrNoPutsOnEncryptedRecord = fmt.Errorf("directly put-ting to this lexicon is not valid")
	ErrNotLocalRepo            = fmt.Errorf("the desired did does not live on this repo")
	ErrUnauthorized            = fmt.Errorf("unauthorized request")
)

func NewPear(
	ctx context.Context,
	domain string,
	serviceName string,
	dir identity.Directory,
	perms permissions.Store,
	repo *repo,
	inbox inbox.Inbox,
) *Pear {
	return &Pear{
		ctx:         ctx,
		url:         "https://" + domain, // We use https
		serviceName: serviceName,
		dir:         dir,
		permissions: perms,
		repo:        repo,
		inbox:       inbox,
	}
}

// putRecord puts the given record on the repo connected to this permissionEnforcingRepo.
// It does not do any encryption, permissions, auth, etc. It is assumed that only the owner of the store can call this and that
// is gated by some higher up level. This should be re-written in the future to not give any incorrect impression.
func (p *Pear) putRecord(
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
func (p *Pear) getRecord(
	collection string,
	rkey string,
	targetDID syntax.DID,
	callerDID syntax.DID,
) (*Record, error) {
	has, err := p.hasRepoForDid(targetDID)
	if err != nil {
		return nil, err
	}
	if has {
		// Run permissions before returning to the user
		authz, err := p.permissions.HasPermission(
			callerDID.String(),
			targetDID.String(),
			collection,
			rkey,
		)
		if err != nil {
			return nil, err
		}

		if authz {
			// User has permission, return the record
			return p.repo.getRecord(targetDID.String(), collection, rkey)
		}
		// User doesn't have permission, continue to check notifications
	}

	// Step 2: Search through notifications for records the user has access to
	var joinedResults []struct {
		inbox.Notification
		Record
	}

	err = p.repo.db.Table("notifications").
		Select("notifications.*, records.*").
		Joins("INNER JOIN records ON notifications.sender = records.did AND notifications.collection = records.collection AND notifications.rkey = records.rkey").
		Where("notifications.recipient = ?", callerDID.String()).
		Where("notifications.collection = ?", collection).
		Where("notifications.rkey = ?", rkey).
		Find(&joinedResults).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query notifications with records: %w", err)
	}

	// There should only be one notification per record max
	if len(joinedResults) > 1 {
		return nil, fmt.Errorf("multiple notifications found for record %s/%s/%s", collection, rkey, callerDID.String())
	}

	// Step 3: If we found a notification, check if Sender is local and check permissions
	if len(joinedResults) == 1 {
		result := joinedResults[0]
		originDid := result.Notification.Sender

		// Check if the record owner is on the same node
		hasLocalRepo, err := p.hasRepoForDid(syntax.DID(originDid))
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
			callerDID.String(),
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

func (p *Pear) listRecords(
	did syntax.DID,
	collection string,
	callerDID syntax.DID,
) ([]Record, error) {
	var allRecords []Record
	// Step 1: Check if the requested repo exists (only validate if params.Repo is different from caller and is a valid DID)
	if did != callerDID {
		// Try to parse as DID - if it fails, assume it's the caller's repo (for backward compatibility)
		hasRequestedRepo, err := p.hasRepoForDid(did)
		if err != nil {
			// If DID not found, return error
			return nil, err
		}
		if !hasRequestedRepo {
			return nil, ErrNotLocalRepo
		}
		// If params.Repo doesn't parse as a DID, we'll assume it's valid and continue
		// (for backward compatibility with tests that use simple strings)
	}

	// Step 2: Get records from caller's own repo (if it exists locally)
	hasOwnRepo, err := p.hasRepoForDid(callerDID)
	if err != nil {
		return nil, err
	}
	if !hasOwnRepo {
		return nil, ErrNotLocalRepo
	}
	allow, deny, err := p.permissions.ListReadPermissionsByUser(
		did.String(),
		callerDID.String(),
		collection,
	)
	if err != nil {
		return nil, err
	}

	ownRecords, err := p.repo.listRecords(callerDID.String(), collection, allow, deny)
	if err != nil {
		return nil, err
	}
	allRecords = append(allRecords, ownRecords...)

	// Step 3: Query notifications JOINed with records table
	// This automatically filters out notifications without corresponding records
	var joinedResults []struct {
		inbox.Notification
		Record
	}

	err = p.repo.db.Table("notifications").
		Select("notifications.*, records.*").
		Joins("INNER JOIN records ON notifications.sender = records.did AND notifications.collection = records.collection AND notifications.rkey = records.rkey").
		Where("notifications.recipient = ?", callerDID.String()).
		Where("notifications.collection = ?", collection).
		Find(&joinedResults).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query notifications with records: %w", err)
	}

	// Step 4: For each joined result, check if Sender is local and check permissions
	for _, result := range joinedResults {
		originDid := syntax.DID(result.Notification.Sender)

		// Check if the record owner is on the same node
		hasLocalRepo, err := p.hasRepoForDid(originDid)
		if err != nil {
			// If DID not found, skip this record (it's from a different node)
			if errors.Is(err, identity.ErrDIDNotFound) {
				log.Warn().
					Str("originDid", originDid.String()).
					Str("collection", result.Notification.Collection).
					Str("rkey", result.Notification.Rkey).
					Msg("skipping record from different node (DID not found)")
				continue
			}
			return nil, err
		}

		if !hasLocalRepo {
			// Record is on a different node - log warning and skip
			log.Warn().
				Str("originDid", originDid.String()).
				Str("collection", result.Notification.Collection).
				Str("rkey", result.Notification.Rkey).
				Msg("skipping record from different node (cross-node fetching not yet implemented)")
			continue
		}

		// Record is on the same node - check permissions
		authz, err := p.permissions.HasPermission(
			callerDID.String(),
			originDid.String(),
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

var (
	ErrNoHabitatServer = errors.New("no habitat server found for did :%s")
)

func (p *Pear) hasRepoForDid(did syntax.DID) (bool, error) {
	id, err := p.dir.LookupDID(p.ctx, did)
	if err != nil {
		return false, err
	}

	found, ok := id.Services[p.serviceName]
	if !ok {
		return false, fmt.Errorf(ErrNoHabitatServer.Error(), did.String())
	}

	return found.URL == p.url, nil
}

// TODO: actually enforce permissions here
func (p *Pear) getBlob(
	did string,
	cid string,
) (string /* mimetype */, []byte /* raw blob */, error) {
	return p.repo.getBlob(did, cid)
}

// TODO: actually enforce permissions here
func (p *Pear) uploadBlob(did string, data []byte, mimeType string) (*blob, error) {
	return p.repo.uploadBlob(did, data, mimeType)
}

func (p *Pear) notifyOfUpdate(ctx context.Context, sender syntax.DID, recipient syntax.DID, collection string, rkey string) error {
	return p.inbox.PutNotification(ctx, sender, recipient, collection, rkey)
}
