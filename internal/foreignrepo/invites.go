// Package foreignrepo stores authored records destined for spaces whose
// authority is not hosted here. It does not register or serve any endpoint.
package foreignrepo

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/lexicon"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	atgrp "github.com/habitat-network/habitat/api/atgrp"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/spacecommit"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	InboxType        = "org.atgrp.inbox"
	InviteCollection = "org.atgrp.invite"
	maxInviteBytes   = 24 << 10
	maxRepoInvites   = 256
)

var (
	ErrScope         = errors.New("not a locally authored foreign invite repository")
	ErrRepoFull      = errors.New("foreign invite repository is full")
	ErrRecordChanged = errors.New("foreign invite CID changed")
	inviteText       = lexicon.SchemaString{MaxLength: new(20000), MaxGraphemes: new(2000)}
	recordPrefix     = cid.NewPrefixV1(cid.DagCBOR, multihash.SHA2_256)
)

type repoKey struct {
	SpaceOwner syntax.DID              `gorm:"primaryKey;not null"`
	SpaceType  syntax.NSID             `gorm:"primaryKey;not null"`
	SpaceSkey  habitat_syntax.SpaceKey `gorm:"primaryKey;not null"`
	Author     syntax.DID              `gorm:"primaryKey;not null"`
}

type inviteRepo struct {
	Key   repoKey    `gorm:"embedded"`
	Rev   syntax.TID `gorm:"not null"`
	State []byte     `gorm:"not null"`
}

func (inviteRepo) TableName() string { return "foreign_invite_repos" }

type inviteRecord struct {
	Key   repoKey          `gorm:"embedded"`
	Rkey  syntax.RecordKey `gorm:"primaryKey;not null"`
	CID   string           `gorm:"column:cid;not null"`
	Value []byte           `gorm:"not null"`
}

func (inviteRecord) TableName() string { return "foreign_invite_records" }

// Models returns the tables private to this store. The collection is
// implicit: only org.atgrp.invite records can be stored.
func Models() []any { return []any{&inviteRepo{}, &inviteRecord{}, &inviteDelivery{}} }

// Store enforces local author custody and a foreign authority. A caller
// must separately authorize the acting user to act as that author.
type Store struct {
	db          *gorm.DB
	local       hive.Hive
	localSpaces spaces.Store
}

func New(db *gorm.DB, local hive.Hive, localSpaces spaces.Store) *Store {
	return &Store{db: db, local: local, localSpaces: localSpaces}
}

// WithTx lets a caller compose a write and its queued notification with
// other changes. Writes themselves send nothing.
func (s *Store) WithTx(tx *gorm.DB) *Store {
	return New(tx, s.local, s.localSpaces)
}

type scope struct {
	inbox  habitat_syntax.SpaceURI
	author syntax.DID
}

func parseScope(inbox habitat_syntax.SpaceURI, author syntax.DID) (scope, error) {
	uri, err := habitat_syntax.ParseSpaceURI(inbox.String())
	if err != nil || uri.SpaceType() != InboxType || uri.Skey() != "self" ||
		uri.SpaceOwner() == author {
		return scope{}, ErrScope
	}
	if _, err := syntax.ParseDID(author.String()); err != nil {
		return scope{}, ErrScope
	}
	return scope{inbox: uri, author: author}, nil
}

func parseRecord(uri habitat_syntax.SpaceRecordURI) (scope, syntax.RecordKey, error) {
	parsed, err := habitat_syntax.ParseSpaceRecordURI(uri.String())
	if err != nil || parsed.Collection() != InviteCollection {
		return scope{}, "", ErrScope
	}
	if _, err := syntax.ParseTID(parsed.Rkey().String()); err != nil {
		return scope{}, "", ErrScope
	}
	repo, err := parseScope(parsed.SpaceURI(), parsed.Repo())
	return repo, parsed.Rkey(), err
}

func (k scope) match(db *gorm.DB) *gorm.DB {
	return db.Scopes(habitat_syntax.InSpace(k.inbox)).Where("author = ?", k.author)
}

func (k scope) key() repoKey {
	ref := k.inbox.Ref()
	return repoKey{
		SpaceOwner: ref.Owner,
		SpaceType:  ref.Type,
		SpaceSkey:  ref.Skey,
		Author:     k.author,
	}
}

func (s *Store) checkCustody(ctx context.Context, tx *gorm.DB, k scope) error {
	local := s.local.WithTx(tx)
	author, err := local.LookupDID(ctx, k.author)
	if errors.Is(err, identity.ErrDIDNotFound) || (err == nil && author.DID != k.author) {
		return ErrScope
	}
	if err != nil {
		return fmt.Errorf("look up invite author: %w", err)
	}
	_, err = local.LookupDID(ctx, k.inbox.SpaceOwner())
	if err == nil {
		return ErrScope
	}
	if !errors.Is(err, identity.ErrDIDNotFound) {
		return fmt.Errorf("look up inbox authority: %w", err)
	}
	exists, err := s.localSpaces.WithTx(tx).CheckSpaceExists(ctx, k.inbox)
	if err != nil {
		return err
	}
	if exists {
		return ErrScope
	}
	return nil
}

// Apply mutates at most one invite. Create requires a fresh TID, and update
// and delete must name the current CID. Identical updates don't advance the
// head. Deleting the last record keeps a new, verifiable empty commit.
// Every changed revision queues its notification in the same transaction.
func (s *Store) Apply(
	ctx context.Context, uri habitat_syntax.SpaceRecordURI, action spaces.WriteAction,
	value *atgrp.OrgAtgrpInvite, swapCID string,
) (spacecommit.Commit, error) {
	k, rkey, err := parseRecord(uri)
	if err != nil {
		return spacecommit.Commit{}, err
	}
	encoded, newCID, err := prepareWrite(action, value, swapCID)
	if err != nil {
		return spacecommit.Commit{}, err
	}
	var commit spacecommit.Commit
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Insert first to reserve SQLite's writer before reading. On Postgres
		// this creates the row to lock, even for two racing first writes.
		head := inviteRepo{Key: k.key(), State: make([]byte, spacecommit.LtHashStateBytes)}
		inserted := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&head)
		if inserted.Error != nil {
			return inserted.Error
		}
		if err := s.checkCustody(ctx, tx, k); err != nil {
			return err
		}
		head, err := readHead(tx, k, "UPDATE")
		if err != nil {
			return err
		}
		if _, err := syntax.ParseTID(head.Rev.String()); err != nil &&
			(head.Rev != "" || inserted.RowsAffected == 0) {
			return spacecommit.ErrInvalidCommit
		}
		state := spacecommit.Load(head.State)
		changed, err := writeRecord(tx, k, rkey, action, encoded, newCID, swapCID, &state)
		if err != nil {
			return err
		}
		if changed {
			clock := syntax.ClockFromTID(head.Rev)
			rev := clock.Next()
			if rev <= head.Rev {
				return spacecommit.ErrInvalidCommit
			}
			head.Rev = rev
			if err := k.match(tx.Model(&inviteRepo{})).
				Updates(map[string]any{"rev": head.Rev, "state": state.State()}).Error; err != nil {
				return err
			}
		}
		commit = spacecommit.New(k.inbox, k.author, head.Rev.String(), state.Sum())
		if changed {
			return enqueue(tx, k, commit, time.Now())
		}
		return nil
	})
	if err != nil {
		return spacecommit.Commit{}, fmt.Errorf("write foreign invite: %w", err)
	}
	return commit, nil
}

func prepareWrite(
	action spaces.WriteAction, value *atgrp.OrgAtgrpInvite, swapCID string,
) ([]byte, string, error) {
	switch action {
	case spaces.WriteCreate:
		if swapCID != "" {
			return nil, "", spaces.ErrInvalidRecord
		}
	case spaces.WriteUpdate, spaces.WriteDelete:
		if swapCID == "" || len(swapCID) > 128 {
			return nil, "", spaces.ErrInvalidRecord
		}
		parsed, err := cid.Parse(swapCID)
		if err != nil || parsed.String() != swapCID {
			return nil, "", spaces.ErrInvalidRecord
		}
		prefix := parsed.Prefix()
		if prefix.Version != 1 || prefix.Codec != cid.DagCBOR ||
			prefix.MhType != multihash.SHA2_256 || prefix.MhLength != 32 {
			return nil, "", spaces.ErrInvalidRecord
		}
	default:
		return nil, "", spaces.ErrInvalidRecord
	}
	if action == spaces.WriteDelete {
		if value != nil {
			return nil, "", spaces.ErrInvalidRecord
		}
		return nil, "", nil
	}
	if value == nil || (value.LexiconTypeID != "" && value.LexiconTypeID != InviteCollection) {
		return nil, "", spaces.ErrInvalidRecord
	}
	// Reject oversized input before encoding or grapheme counting.
	if len(value.Message) > 20000 || len(value.CreatedAt) > 64 || len(value.ExpiresAt) > 64 {
		return nil, "", spaces.ErrRecordTooLarge
	}
	created, err := syntax.ParseDatetime(value.CreatedAt)
	if err != nil {
		return nil, "", spaces.ErrInvalidRecord
	}
	if value.ExpiresAt != "" {
		expiry, err := syntax.ParseDatetime(value.ExpiresAt)
		if err != nil || !expiry.Time().After(created.Time()) {
			return nil, "", spaces.ErrInvalidRecord
		}
	}
	if !utf8.ValidString(value.Message) || inviteText.Validate(value.Message, 0) != nil {
		return nil, "", spaces.ErrInvalidRecord
	}
	encoded, err := spaces.MarshalRecord(value)
	if err != nil {
		return nil, "", err
	}
	if len(encoded) > maxInviteBytes {
		return nil, "", spaces.ErrRecordTooLarge
	}
	id, err := recordPrefix.Sum(encoded)
	return encoded, id.String(), err
}

func writeRecord(
	tx *gorm.DB, k scope, rkey syntax.RecordKey, action spaces.WriteAction,
	value []byte, newCID, swapCID string, state *spacecommit.LtHash,
) (bool, error) {
	query := func() *gorm.DB {
		return k.match(tx.Model(&inviteRecord{})).Where("rkey = ?", rkey)
	}
	var existing inviteRecord
	err := query().Take(&existing).Error
	found := err == nil
	if !found && !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	if action == spaces.WriteCreate {
		if found {
			return false, spaces.ErrRecordAlreadyExists
		}
		var count int64
		if err := k.match(tx.Model(&inviteRecord{})).Count(&count).Error; err != nil {
			return false, err
		}
		if count >= maxRepoInvites {
			return false, ErrRepoFull
		}
		if err := tx.Create(&inviteRecord{
			Key: k.key(), Rkey: rkey, CID: newCID, Value: value,
		}).Error; err != nil {
			return false, err
		}
	} else {
		if !found {
			return false, spaces.ErrRecordNotFound
		}
		if existing.CID != swapCID {
			return false, ErrRecordChanged
		}
		if newCID == existing.CID {
			return false, nil
		}
		state.Remove(spacecommit.RecordElement(InviteCollection, rkey, existing.CID))
		if action == spaces.WriteDelete {
			return true, query().Delete(&inviteRecord{}).Error
		}
		if err := query().Updates(map[string]any{"cid": newCID, "value": value}).Error; err != nil {
			return false, err
		}
	}
	state.Add(spacecommit.RecordElement(InviteCollection, rkey, newCID))
	return true, nil
}

func readHead(tx *gorm.DB, k scope, lock string) (inviteRepo, error) {
	q := k.match(tx)
	if tx.Name() == "postgres" {
		q = q.Clauses(clause.Locking{Strength: lock})
	}
	var head inviteRepo
	if err := q.Take(&head).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return inviteRepo{}, spaces.ErrRepoNotFound
	} else if err != nil {
		return inviteRepo{}, err
	}
	if len(head.State) != spacecommit.LtHashStateBytes {
		return inviteRepo{}, spacecommit.ErrInvalidCommit
	}
	return head, nil
}

// Record holds canonical CBOR and its CID at its authored record URI.
type Record struct {
	URI   habitat_syntax.SpaceRecordURI
	CID   cid.Cid
	Value []byte
}

func readRecord(k scope, row inviteRecord) (Record, error) {
	c, err := recordPrefix.Sum(row.Value)
	if err != nil || len(row.Value) > maxInviteBytes || c.String() != row.CID {
		return Record{}, spacecommit.ErrInvalidCommit
	}
	uri := habitat_syntax.ConstructSpaceRecordURI(k.inbox, k.author, InviteCollection, row.Rkey)
	return Record{URI: uri.AtSpace(), CID: c, Value: row.Value}, nil
}

// Get returns the exact live author record, never a local authority or
// replicated record.
func (s *Store) Get(ctx context.Context, uri habitat_syntax.SpaceRecordURI) (Record, error) {
	k, rkey, err := parseRecord(uri)
	if err != nil {
		return Record{}, err
	}
	tx := s.db.WithContext(ctx)
	if err := s.checkCustody(ctx, tx, k); err != nil {
		return Record{}, err
	}
	var row inviteRecord
	if err := k.match(tx).
		Where("rkey = ?", rkey).
		Take(&row).
		Error; errors.Is(
		err,
		gorm.ErrRecordNotFound,
	) {
		return Record{}, spaces.ErrRecordNotFound
	} else if err != nil {
		return Record{}, err
	}
	return readRecord(k, row)
}

// Snapshot reads one consistent commit and record set. It does not authenticate
// the caller or generate a wire MAC; a future serving layer must do both.
func (s *Store) Snapshot(
	ctx context.Context, inbox habitat_syntax.SpaceURI, author syntax.DID,
) (spacecommit.Commit, []Record, error) {
	k, err := parseScope(inbox, author)
	if err != nil {
		return spacecommit.Commit{}, nil, err
	}
	var commit spacecommit.Commit
	var records []Record
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.checkCustody(ctx, tx, k); err != nil {
			return err
		}
		head, err := readHead(tx, k, "SHARE")
		if err != nil {
			return err
		}
		if _, err := syntax.ParseTID(head.Rev.String()); err != nil {
			return spacecommit.ErrInvalidCommit
		}
		var rows []inviteRecord
		if err := k.match(tx).
			Order("rkey").
			Limit(maxRepoInvites + 1).
			Find(&rows).
			Error; err != nil {
			return err
		}
		if len(rows) > maxRepoInvites {
			return ErrRepoFull
		}
		var folded spacecommit.LtHash
		for _, row := range rows {
			record, err := readRecord(k, row)
			if err != nil {
				return err
			}
			folded.Add(spacecommit.RecordElement(InviteCollection, row.Rkey, row.CID))
			records = append(records, record)
		}
		state := spacecommit.Load(head.State)
		commit = spacecommit.New(k.inbox, k.author, head.Rev.String(), state.Sum())
		if !commit.Matches(&folded) {
			return spacecommit.ErrInvalidCommit
		}
		return nil
	})
	if err != nil {
		return spacecommit.Commit{}, nil, err
	}
	return commit, records, nil
}
