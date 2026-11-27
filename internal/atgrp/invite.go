package atgrp

import (
	"context"
	"errors"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	InboxSpaceType   = "org.atgrp.inbox"
	InviteCollection = "org.atgrp.invite"
)

var ErrInvalidInvite = errors.New("invalid invite")

func InboxSpace(owner syntax.DID) habitat_syntax.SpaceURI {
	return habitat_syntax.ConstructSpaceURI(owner, InboxSpaceType, "self")
}

// IsInboxPolicy identifies the private, write-only-to-others inbox policy.
func IsInboxPolicy(policy spaces.Policy) bool {
	return policy.Type == spaces.PolicyMemberSpace && policy.PublicWrite && !policy.PublicRead &&
		len(policy.AllowedApps) == 0 && !policy.PublishMembers && !policy.PublishPolicy
}

// ParseBoundInvite requires the exact invite URI, not just the existence of
// some invite to the group. The recipient comes from the authenticated DID.
func ParseBoundInvite(raw string, group, recipient syntax.DID) (habitat_syntax.SpaceRecordURI, error) {
	uri, err := habitat_syntax.ParseSpaceRecordURI(raw)
	if err != nil || uri.SpaceURI() != InboxSpace(recipient) ||
		uri.Repo() != group || uri.Collection() != InviteCollection {
		return "", ErrInvalidInvite
	}
	if _, err := syntax.ParseTID(uri.Rkey().String()); err != nil {
		return "", ErrInvalidInvite
	}
	return uri, nil
}

// ValidateInviteRecord checks the fields that affect admission. Invite
// records never grant roles. The host selects its configured default roles.
func ValidateInviteRecord(value map[string]any, now time.Time) error {
	if value["$type"] != InviteCollection {
		return ErrInvalidInvite
	}
	created, ok := readInviteTime(value, "createdAt")
	if !ok {
		return ErrInvalidInvite
	}
	if _, present := value["expiresAt"]; present {
		expiry, ok := readInviteTime(value, "expiresAt")
		if !ok || !expiry.After(created) || !expiry.After(now) {
			return ErrInvalidInvite
		}
	}
	if message, present := value["message"]; present {
		text, ok := message.(string)
		if !ok || len(text) > 20000 {
			return ErrInvalidInvite
		}
	}
	return nil
}

func readInviteTime(value map[string]any, key string) (time.Time, bool) {
	raw, ok := value[key].(string)
	if !ok {
		return time.Time{}, false
	}
	timestamp, err := syntax.ParseDatetime(raw)
	if err != nil {
		return time.Time{}, false
	}
	return timestamp.Time(), true
}

// ConsumeInvite deletes a live invite. The store must be scoped to the
// transaction that creates the corresponding membership so a failure does
// not lose the invite. A revoke or another redemption that wins the race
// makes the strict delete fail.
func ConsumeInvite(
	ctx context.Context, store spaces.Store, uri habitat_syntax.SpaceRecordURI, now time.Time,
) error {
	policy, err := store.GetSpacePolicy(ctx, uri.SpaceURI())
	if errors.Is(err, spaces.ErrSpaceNotFound) || (err == nil && !IsInboxPolicy(policy)) {
		return ErrInvalidInvite
	}
	if err != nil {
		return err
	}
	record, err := store.GetRecord(ctx, uri.SpaceURI(), uri.Repo(), uri.Collection(), uri.Rkey())
	if errors.Is(err, spaces.ErrRecordNotFound) || errors.Is(err, spaces.ErrSpaceNotFound) {
		return ErrInvalidInvite
	}
	if err != nil {
		return err
	}
	if err := ValidateInviteRecord(record.Value, now); err != nil {
		return err
	}
	_, err = store.ApplyWrites(ctx, uri.SpaceURI(), uri.Repo(), []spaces.Write{{
		Action:     spaces.WriteDelete,
		Collection: uri.Collection(),
		Rkey:       uri.Rkey(),
		SwapCID:    record.Cid.String(),
	}})
	if errors.Is(err, spaces.ErrRecordNotFound) || errors.Is(err, spaces.ErrSpaceNotFound) ||
		errors.Is(err, spaces.ErrRecordChanged) {
		return ErrInvalidInvite
	}
	return err
}
