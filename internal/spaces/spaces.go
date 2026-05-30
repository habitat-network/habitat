package spaces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/openfga/pkg/tuple"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/habitat-network/habitat/internal/fgastore"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// GORM models

type space struct {
	Owner     syntax.DID              `gorm:"primaryKey"`
	Type      syntax.NSID             `gorm:"primaryKey"`
	Skey      habitat_syntax.SpaceKey `gorm:"primaryKey"`
	CreatedAt time.Time
	Rev       syntax.TID `gorm:"uniqueIndex"`
	Deleted   bool       `gorm:"default:false"`
}

type spaceRecord struct {
	Space      habitat_syntax.SpaceURI `gorm:"primaryKey"`
	Owner      syntax.DID              `gorm:"primaryKey"`
	Collection syntax.NSID             `gorm:"primaryKey"`
	Rkey       syntax.RecordKey        `gorm:"primaryKey"`
	Value      []byte
	Rev        syntax.TID `gorm:"uniqueIndex"`
	Deleted    bool       `gorm:"default:false"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type spaceMemberOp struct {
	Space     string    `gorm:"primaryKey"`
	Rev       string    `gorm:"primaryKey"`
	Idx       int       `gorm:"primaryKey"`
	Action    string    `gorm:"size:16"`
	DID       string    `gorm:"size:256"`
	Access    *string   `gorm:"size:16"`
	CreatedAt time.Time
}

type spaceEffectiveMember struct {
	Space     string `gorm:"primaryKey"`
	DID       string `gorm:"primaryKey;size:256"`
	Access    string `gorm:"size:16"`
	Rev       string
	UpdatedAt time.Time
}

// SpaceView is the public representation of a space
type SpaceView struct {
	URI         habitat_syntax.SpaceURI
	Type        syntax.NSID
	Skey        habitat_syntax.SpaceKey
	MemberCount int
}

// MemberOp is a single member operation in the oplog
type MemberOp struct {
	Space  string
	Rev    string
	Idx    int
	Action string
	DID    string
	Access *string
}

// RecordChange is a single record change for syncing
type RecordChange struct {
	Space      string
	Rev        string
	Action     string
	Collection string
	Rkey       string
	Value      *map[string]any
}

// SpaceState is the current state of a space for sync
type SpaceState struct {
	Space     string
	SpaceType string
	SpaceRev  string
	MemberRev string
	Repos     []SpaceRepoState
}

// SpaceRepoState is the state of a single repo within a space
type SpaceRepoState struct {
	DID string
	Rev string
}

// MemberInfo holds a member's DID and when they were added
type MemberInfo struct {
	Did     syntax.DID
	Access  SpaceAccess
	AddedAt time.Time
}

// Record is a single record within a space
type Record struct {
	Owner      syntax.DID
	Collection syntax.NSID
	Rkey       syntax.RecordKey
	Value      map[string]any
	Rev        string
	UpdatedAt  time.Time
}

type SpaceAccess string

const (
	SpaceAccessRead  SpaceAccess = "read"
	SpaceAccessWrite SpaceAccess = "write"
)

func ParseSpaceAccess(access string) (SpaceAccess, error) {
	switch access {
	case "read":
		return SpaceAccessRead, nil
	case "write":
		return SpaceAccessWrite, nil
	default:
		return "", fmt.Errorf("unknown space access: %s", access)
	}
}

// Store defines the persistence interface for spaces
type Store interface {
	// Space operations
	CreateSpace(
		ctx context.Context,
		owner syntax.DID,
		spaceType syntax.NSID,
		skey habitat_syntax.SpaceKey,
	) (habitat_syntax.SpaceURI, error)
	// ListSpaces returns the spaces that `member` is a part of
	ListSpaces(
		ctx context.Context,
		member syntax.DID,
		filterOwner *syntax.DID,
		filterType *syntax.NSID,
	) ([]SpaceView, error)

	// Member operations
	AddMember(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		did syntax.DID,
		access SpaceAccess,
	) (string, error)
	RemoveMember(ctx context.Context, space habitat_syntax.SpaceURI, did syntax.DID) (string, error)
	GetMembers(ctx context.Context, space habitat_syntax.SpaceURI) ([]MemberInfo, error)
	IsMember(ctx context.Context, space habitat_syntax.SpaceURI, did syntax.DID) (bool, error)

	// Record operations
	PutRecord(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		owner syntax.DID,
		collection syntax.NSID,
		rkey syntax.RecordKey,
		value map[string]any,
	) (syntax.TID, error)
	GetRecord(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		owner syntax.DID,
		collection syntax.NSID,
		rkey syntax.RecordKey,
	) (*Record, error)
	ListRecords(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		repo syntax.DID,
		collection *syntax.NSID,
	) ([]Record, error)
	DeleteRecord(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		collection syntax.NSID,
		rkey string,
	) (syntax.TID, error)
	DeleteSpace(ctx context.Context, uri habitat_syntax.SpaceURI) (syntax.TID, error)

	// Oplog operations
	GetRepoOplog(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		repo syntax.DID,
		since string,
		limit int,
	) ([]Record, error)
	ListRecordChanges(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		repo string,
		since string,
		limit int,
	) ([]RecordChange, error)
	GetMemberOplog(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		since string,
		limit int,
	) ([]MemberOp, error)
	GetSpaceState(ctx context.Context, space habitat_syntax.SpaceURI) (*SpaceState, error)
}

var (
	ErrSpaceNotFound      = errors.New("space not found")
	ErrSpaceAlreadyExists = errors.New("space already exists")
	ErrRecordNotFound     = errors.New("record not found")
	ErrUserAlreadyMember  = errors.New("user is already a member of the space")
	ErrNotAMember         = errors.New("user is not a member of the space")
	ErrCannotRemoveOwner  = errors.New("cannot remove the owner of a space")
)

// ---- Store implementation ----

type store struct {
	db    *gorm.DB
	fga   fgastore.Store
	clock *syntax.TIDClock
}

var _ Store = &store{}

func NewStore(db *gorm.DB, fga fgastore.Store) (*store, error) {
	if err := db.AutoMigrate(&space{}, &spaceRecord{}, &spaceMemberOp{}, &spaceEffectiveMember{}); err != nil {
		return nil, fmt.Errorf("failed to migrate spaces tables: %w", err)
	}
	return &store{db: db, fga: fga, clock: syntax.NewTIDClock(0)}, nil
}

// ownerContextualTuple returns a Tuple representing the owner relationship,
// for use as a contextual tuple in FGA queries.  This is how we make the
// owner a recognized member of the space without storing the owner in FGA.
func ownerContextualTuple(uri habitat_syntax.SpaceURI) fgastore.Tuple {
	return fgastore.Tuple{
		User:     fgastore.MemberUserString(uri.SpaceDID()),
		Relation: fgastore.RelationSpaceOwner,
		Object:   fgastore.SpaceObjectKey(uri),
	}
}

func (s *store) latestMemberRev(ctx context.Context, uri habitat_syntax.SpaceURI) (string, error) {
	var rev struct {
		Rev string
	}
	err := s.db.WithContext(ctx).
		Model(&spaceMemberOp{}).
		Select("rev").
		Where("space = ?", uri.String()).
		Order("rev DESC").
		Limit(1).
		Scan(&rev).Error
	if err != nil {
		return "", err
	}
	return rev.Rev, nil
}

func (s *store) reconcileEffectiveMembers(ctx context.Context, uri habitat_syntax.SpaceURI, rev string) error {
	members, err := s.GetMembers(ctx, uri)
	if err != nil {
		return fmt.Errorf("get members: %w", err)
	}

	var current []spaceEffectiveMember
	if err := s.db.WithContext(ctx).
		Where("space = ?", uri.String()).
		Find(&current).Error; err != nil {
		return fmt.Errorf("read current: %w", err)
	}

	currentMap := make(map[string]string)
	for _, m := range current {
		currentMap[m.DID] = m.Access
	}

	newMap := make(map[string]string)
	for _, m := range members {
		newMap[string(m.Did)] = string(m.Access)
	}

	tx := s.db.WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	idx := 0
	for did, access := range newMap {
		if currentMap[did] != access {
			a := access
			if err := tx.Create(&spaceMemberOp{
				Space:  uri.String(),
				Rev:    rev,
				Idx:    idx,
				Action: "add",
				DID:    did,
				Access: &a,
			}).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("insert memberop: %w", err)
			}
			idx++
		}
	}

	for did := range currentMap {
		if _, exists := newMap[did]; !exists {
			if err := tx.Create(&spaceMemberOp{
				Space:  uri.String(),
				Rev:    rev,
				Idx:    idx,
				Action: "remove",
				DID:    did,
			}).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("insert memberop remove: %w", err)
			}
			idx++
		}
	}

	if err := tx.Where("space = ?", uri.String()).Delete(&spaceEffectiveMember{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("clear effective: %w", err)
	}

	for _, m := range members {
		access := string(m.Access)
		if err := tx.Create(&spaceEffectiveMember{
			Space:  uri.String(),
			DID:    string(m.Did),
			Access: access,
			Rev:    rev,
		}).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("insert effective: %w", err)
		}
	}

	return tx.Commit().Error
}

func (s *store) CreateSpace(
	ctx context.Context,
	owner syntax.DID,
	spaceType syntax.NSID,
	skey habitat_syntax.SpaceKey,
) (habitat_syntax.SpaceURI, error) {
	if skey == "" {
		skey = habitat_syntax.NewSkey()
	}

	var existing space
	err := s.db.WithContext(ctx).
		Where("owner = ? AND skey = ?", owner, skey).
		First(&existing).
		Error
	if err == nil {
		return "", ErrSpaceAlreadyExists
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}

	sp := space{
		Owner: owner,
		Skey:  skey,
		Type:  spaceType,
		Rev:   s.clock.Next(),
	}

	err = s.db.WithContext(ctx).Create(&sp).Error
	if err != nil {
		return "", err
	}

	return habitat_syntax.ConstructSpaceURI(owner, spaceType, skey), nil
}

func (s *store) ListSpaces(
	ctx context.Context,
	member syntax.DID,
	filterOwner *syntax.DID,
	filterType *syntax.NSID,
) ([]SpaceView, error) {
	var allRows []space
	// start with owned spaces
	ownedRowsQuery := s.db.WithContext(ctx).Model(&space{}).Where("owner = ? AND deleted = ?", member, false)
	if filterOwner != nil {
		ownedRowsQuery = ownedRowsQuery.Where("owner = ?", *filterOwner)
	}
	if filterType != nil {
		ownedRowsQuery = ownedRowsQuery.Where("type = ?", *filterType)
	}
	if err := ownedRowsQuery.Find(&allRows).Error; err != nil {
		return nil, fmt.Errorf("list owned spaces: %w", err)
	}
	// then check fga store for memberships
	fgaObjects, err := s.fga.ListObjects(
		ctx,
		fgastore.MemberUserString(member),
		"can_read",
		"space",
	)
	if err != nil {
		return nil, fmt.Errorf("list fga member spaces: %w", err)
	}
	if len(fgaObjects) > 0 {
		conditions := s.db
		for _, key := range fgaObjects {
			uri, err := fgastore.ParseSpaceObjectKey(key)
			if err != nil {
				return nil, fmt.Errorf("parse space object key: %w", err)
			}
			conditions = conditions.Or("owner = ? AND skey = ?", uri.SpaceDID(), uri.Skey())
		}
		query := s.db.WithContext(ctx).Model(&space{}).Where(conditions).Where("deleted = ?", false)
		if filterOwner != nil {
			query = query.Where("owner = ?", *filterOwner)
		}
		if filterType != nil {
			query = query.Where("type = ?", *filterType)
		}

		var otherRows []space
		if err := query.Order("created_at DESC").Find(&otherRows).Error; err != nil {
			return nil, err
		}

		allRows = append(allRows, otherRows...)
	}

	log.Debug().Int("spaces", len(allRows)).Msg("list spaces")

	views := make([]SpaceView, len(allRows))
	for i, row := range allRows {
		uri := habitat_syntax.ConstructSpaceURI(row.Owner, row.Type, row.Skey)
		fgaUsers, err := s.fga.ListUsers(
			ctx,
			fgastore.SpaceObjectKey(uri),
			"can_read",
			ownerContextualTuple(uri),
		)
		memberCount := 0
		if err == nil {
			memberCount = len(fgaUsers)
		}
		views[i] = SpaceView{
			URI:         uri,
			Type:        row.Type,
			Skey:        row.Skey,
			MemberCount: memberCount,
		}
	}
	log.Debug().Int("views", len(views)).Msg("list spaces")
	return views, nil
}

func (s *store) GetMembers(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
) ([]MemberInfo, error) {
	var sp space
	err := s.db.WithContext(ctx).
		Where("owner = ? AND skey = ? AND deleted = ?", uri.SpaceDID(), uri.Skey(), false).
		First(&sp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSpaceNotFound
	} else if err != nil {
		return nil, err
	}

	readerStrs, err := s.fga.ListUsers(
		ctx,
		fgastore.SpaceObjectKey(uri),
		"can_read",
		ownerContextualTuple(uri),
	)
	if err != nil {
		return nil, err
	}

	writerStrs, err := s.fga.ListUsers(
		ctx,
		fgastore.SpaceObjectKey(uri),
		"can_write",
		ownerContextualTuple(uri),
	)
	if err != nil {
		return nil, err
	}
	writerSet := make(map[string]struct{}, len(writerStrs))
	for _, w := range writerStrs {
		writerSet[w] = struct{}{}
	}

	members := make([]MemberInfo, 0, len(readerStrs))
	for _, userStr := range readerStrs {
		did, err := fgastore.MemberUserToDID(userStr)
		if err != nil {
			continue
		}
		access := SpaceAccessRead
		if _, ok := writerSet[userStr]; ok {
			access = SpaceAccessWrite
		}
		members = append(members, MemberInfo{Did: did, Access: access})
	}

	return members, nil
}

func (s *store) IsMember(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	did syntax.DID,
) (bool, error) {
	return s.fga.Check(
		ctx,
		fgastore.MemberUserString(did),
		fgastore.RelationSpaceReader,
		fgastore.SpaceObjectKey(uri),
		ownerContextualTuple(uri),
	)
}

func (s *store) AddMember(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	did syntax.DID,
	access SpaceAccess,
) (string, error) {
	var sp space
	err := s.db.WithContext(ctx).
		Where("owner = ? AND skey = ? AND deleted = ?", uri.SpaceDID(), uri.Skey(), false).
		First(&sp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrSpaceNotFound
	} else if err != nil {
		return "", err
	}
	if did == uri.SpaceDID() {
		return "", nil
	}
	if access == SpaceAccessRead {
		if err := s.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
			Writes: &openfgav1.WriteRequestWrites{
				TupleKeys: []*openfgav1.TupleKey{
					tuple.NewTupleKey(
						fgastore.SpaceObjectKey(uri),
						fgastore.RelationSpaceReader,
						fgastore.MemberUserString(did),
					),
				},
				OnDuplicate: "ignore",
			},
			Deletes: &openfgav1.WriteRequestDeletes{
				TupleKeys: []*openfgav1.TupleKeyWithoutCondition{
					tuple.TupleKeyToTupleKeyWithoutCondition(tuple.NewTupleKey(
						fgastore.SpaceObjectKey(uri),
						fgastore.RelationSpaceWriter,
						fgastore.MemberUserString(did),
					)),
				},
				OnMissing: "ignore",
			},
		}); err != nil {
			return "", err
		}
	} else {
		if err := s.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
			Writes: &openfgav1.WriteRequestWrites{
				TupleKeys: []*openfgav1.TupleKey{
					tuple.NewTupleKey(
						fgastore.SpaceObjectKey(uri),
						fgastore.RelationSpaceWriter,
						fgastore.MemberUserString(did),
					),
				},
				OnDuplicate: "ignore",
			},
			Deletes: &openfgav1.WriteRequestDeletes{
				TupleKeys: []*openfgav1.TupleKeyWithoutCondition{
					tuple.TupleKeyToTupleKeyWithoutCondition(tuple.NewTupleKey(
						fgastore.SpaceObjectKey(uri),
						fgastore.RelationSpaceReader,
						fgastore.MemberUserString(did),
					)),
				},
				OnMissing: "ignore",
			},
		}); err != nil {
			return "", err
		}
	}

	rev := s.clock.Next().String()
	if err := s.reconcileEffectiveMembers(ctx, uri, rev); err != nil {
		return "", fmt.Errorf("reconcile effective members: %w", err)
	}
	return rev, nil
}

func (s *store) RemoveMember(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	did syntax.DID,
) (string, error) {
	if did == uri.SpaceDID() {
		return "", ErrCannotRemoveOwner
	}

	var sp space
	err := s.db.WithContext(ctx).
		Where("owner = ? AND skey = ? AND deleted = ?", uri.SpaceDID(), uri.Skey(), false).
		First(&sp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrSpaceNotFound
	} else if err != nil {
		return "", err
	}

	if err := s.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
		Deletes: &openfgav1.WriteRequestDeletes{
			TupleKeys: []*openfgav1.TupleKeyWithoutCondition{
				tuple.TupleKeyToTupleKeyWithoutCondition(tuple.NewTupleKey(
					fgastore.SpaceObjectKey(uri),
					fgastore.RelationSpaceReader,
					fgastore.MemberUserString(did),
				)),
				tuple.TupleKeyToTupleKeyWithoutCondition(tuple.NewTupleKey(
					fgastore.SpaceObjectKey(uri),
					fgastore.RelationSpaceWriter,
					fgastore.MemberUserString(did),
				)),
			},
			OnMissing: "ignore",
		},
	}); err != nil {
		return "", err
	}

	rev := s.clock.Next().String()
	if err := s.reconcileEffectiveMembers(ctx, uri, rev); err != nil {
		return "", fmt.Errorf("reconcile effective members: %w", err)
	}
	return rev, nil
}

// ---- Record operations ----

func (s *store) PutRecord(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	owner syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
	value map[string]any,
) (syntax.TID, error) {
	var sp space
	err := s.db.WithContext(ctx).
		Where("owner = ? AND skey = ? AND deleted = ?", uri.SpaceDID(), uri.Skey(), false).
		First(&sp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrSpaceNotFound
	} else if err != nil {
		return "", err
	}

	bytes, err := json.Marshal(value)
	if err != nil {
		return "", err
	}

	tid := s.clock.Next()
	if rkey == "" {
		rkey = syntax.RecordKey(tid)
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rec := spaceRecord{
			Owner:      owner,
			Space:      uri,
			Collection: collection,
			Rkey:       rkey,
			Value:      bytes,
			Rev:        tid,
			Deleted:    false,
		}

		if err := tx.
			Clauses(clause.OnConflict{UpdateAll: true}).
			Create(&rec).Error; err != nil {
			return err
		}

		return tx.
			Model(&space{}).
			Where("owner = ? AND skey = ?", uri.SpaceDID(), uri.Skey()).
			Update("rev", tid).Error
	})
	if err != nil {
		return "", err
	}
	return tid, nil
}

func (s *store) GetRecord(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	owner syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
) (*Record, error) {
	var row spaceRecord
	err := s.db.WithContext(ctx).
		Where("space = ? AND owner = ? AND collection = ? AND rkey = ? AND deleted = ?",
			uri, owner, collection, rkey, false).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrRecordNotFound
	} else if err != nil {
		return nil, err
	}

	var value map[string]any
	if err := json.Unmarshal(row.Value, &value); err != nil {
		return nil, err
	}

	return &Record{
		Collection: collection,
		Rkey:       row.Rkey,
		Value:      value,
		Rev:        string(row.Rev),
		UpdatedAt:  row.UpdatedAt,
	}, nil
}

func (s *store) ListRecords(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	repo syntax.DID,
	collection *syntax.NSID,
) ([]Record, error) {
	var sp space
	err := s.db.WithContext(ctx).
		Where("owner = ? AND skey = ? AND deleted = ?", uri.SpaceDID(), uri.Skey(), false).
		First(&sp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSpaceNotFound
	} else if err != nil {
		return nil, err
	}

	query := s.db.WithContext(ctx).
		Where("space = ?", uri).
		Where("owner = ?", repo).
		Where("deleted = ?", false)

	if collection != nil {
		query = query.Where("collection = ?", collection)
	}

	var rows []spaceRecord
	if err := query.Order("rkey ASC").Find(&rows).Error; err != nil {
		return nil, err
	}

	records := make([]Record, len(rows))
	for i, row := range rows {
		var value map[string]any
		if err := json.Unmarshal(row.Value, &value); err != nil {
			return nil, err
		}
		records[i] = Record{
			Owner:      row.Owner,
			Collection: row.Collection,
			Rkey:       row.Rkey,
			Value:      value,
			Rev:        string(row.Rev),
			UpdatedAt:  row.UpdatedAt,
		}
	}

	return records, nil
}

func (s *store) DeleteSpace(ctx context.Context, uri habitat_syntax.SpaceURI) (syntax.TID, error) {
	// read the stored FGA tuples for this space before deleting anything,
	// so we know exactly what tuples to delete
	tuples, err := s.fga.Read(ctx, fgastore.Tuple{Object: fgastore.SpaceObjectKey(uri)})
	if err != nil {
		return "", err
	}

	rev := s.clock.Next()

	// everything after this point is idempotent — use a transaction
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.
			Model(&space{}).
			Where("owner = ? AND skey = ?", uri.SpaceDID(), uri.Skey()).
			Updates(map[string]any{
				"deleted": true,
				"rev":     rev,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrSpaceNotFound
		}

		if err := tx.
			Model(&spaceRecord{}).
			Where("space = ?", uri).
			Updates(map[string]any{
				"deleted":    true,
				"value":      nil,
				"updated_at": time.Now(),
			}).Error; err != nil {
			return err
		}

		// delete all stored FGA tuples for this space
		var deletes []*openfgav1.TupleKeyWithoutCondition
		for _, t := range tuples {
			deletes = append(deletes, tuple.TupleKeyToTupleKeyWithoutCondition(
				tuple.NewTupleKey(t.Object, t.Relation, t.User),
			))
		}
		if len(deletes) > 0 {
			return s.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
				Deletes: &openfgav1.WriteRequestDeletes{
					TupleKeys: deletes,
					OnMissing: "ignore",
				},
			})
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return rev, nil
}

// GetRepoOplog returns records ordered by rev for incremental sync.
// It intentionally includes deleted (tombstone) rows so sync clients can
// detect and replay deletions.
func (s *store) GetRepoOplog(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	owner syntax.DID,
	since string,
	limit int,
) ([]Record, error) {
	query := s.db.WithContext(ctx).
		Model(&spaceRecord{}).
		Where("space = ? AND owner = ?", uri, owner)

	if since != "" {
		query = query.Where("rev > ?", since)
	}

	if limit <= 0 {
		limit = 100
	}

	var rows []spaceRecord
	if err := query.Order("rev ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("get repo oplog: %w", err)
	}

	records := make([]Record, len(rows))
	for i, row := range rows {
		var value map[string]any
		if err := json.Unmarshal(row.Value, &value); err != nil {
			return nil, err
		}
		records[i] = Record{
			Owner:      row.Owner,
			Collection: row.Collection,
			Rkey:       row.Rkey,
			Value:      value,
			Rev:        string(row.Rev),
			UpdatedAt:  row.UpdatedAt,
		}
	}
	return records, nil
}

func (s *store) ListRecordChanges(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	repo string,
	since string,
	limit int,
) ([]RecordChange, error) {
	query := s.db.WithContext(ctx).
		Model(&spaceRecord{}).
		Where("space = ?", uri.String())

	if repo != "" {
		query = query.Where("owner = ?", repo)
	}

	if since != "" {
		query = query.Where("rev > ?", since)
	}

	if limit <= 0 {
		limit = 100
	}

	var rows []spaceRecord
	if err := query.Order("rev ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list record changes: %w", err)
	}

	changes := make([]RecordChange, len(rows))
	for i, row := range rows {
		ch := RecordChange{
			Space:      row.Space.String(),
			Rev:        row.Rev.String(),
			Collection: row.Collection.String(),
			Rkey:       row.Rkey.String(),
		}
		if row.Deleted {
			ch.Action = "delete"
		} else {
			ch.Action = "upsert"
			var value map[string]any
			if err := json.Unmarshal(row.Value, &value); err != nil {
				return nil, err
			}
			ch.Value = &value
		}
		changes[i] = ch
	}
	return changes, nil
}

func (s *store) GetMemberOplog(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	since string,
	limit int,
) ([]MemberOp, error) {
	query := s.db.WithContext(ctx).
		Model(&spaceMemberOp{}).
		Where("space = ?", uri.String())

	if since != "" {
		query = query.Where("rev > ?", since)
	}

	if limit <= 0 {
		limit = 100
	}

	var rows []spaceMemberOp
	if err := query.Order("rev ASC, idx ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("get member oplog: %w", err)
	}

	ops := make([]MemberOp, len(rows))
	for i, row := range rows {
		op := MemberOp{
			Space:  row.Space,
			Rev:    row.Rev,
			Idx:    row.Idx,
			Action: row.Action,
			DID:    row.DID,
		}
		if row.Access != nil {
			a := *row.Access
			op.Access = &a
		}
		ops[i] = op
	}
	return ops, nil
}

func (s *store) GetSpaceState(ctx context.Context, uri habitat_syntax.SpaceURI) (*SpaceState, error) {
	var sp space
	err := s.db.WithContext(ctx).
		Where("owner = ? AND skey = ?", uri.SpaceDID(), uri.Skey()).
		First(&sp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if sp.Deleted {
		return nil, nil
	}

	memberRev, err := s.latestMemberRev(ctx, uri)
	if err != nil {
		return nil, err
	}

	var repoRows []SpaceRepoState
	rows, err := s.db.WithContext(ctx).
		Raw("SELECT owner AS did, MAX(rev) AS rev FROM space_records WHERE space = ? GROUP BY owner", uri.String()).
		Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var did string
		var rev string
		if err := rows.Scan(&did, &rev); err != nil {
			return nil, err
		}
		repoRows = append(repoRows, SpaceRepoState{DID: did, Rev: rev})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &SpaceState{
		Space:     uri.String(),
		SpaceType: sp.Type.String(),
		SpaceRev:  sp.Rev.String(),
		MemberRev: memberRev,
		Repos:     repoRows,
	}, nil
}

func (s *store) DeleteRecord(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	collection syntax.NSID,
	rkey string,
) (syntax.TID, error) {
	tid := s.clock.Next()

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.
			Model(&spaceRecord{}).
			Where("space = ? AND collection = ? AND rkey = ?", uri, collection, rkey).
			Updates(map[string]any{
				"deleted":    true,
				"rev":        tid,
				"value":      nil,
				"updated_at": time.Now(),
			}).Error; err != nil {
			return err
		}

		return tx.
			Model(&space{}).
			Where("owner = ? AND skey = ?", uri.SpaceDID(), uri.Skey()).
			Update("rev", tid).Error
	})
	if err != nil {
		return "", err
	}
	return tid, nil
}
