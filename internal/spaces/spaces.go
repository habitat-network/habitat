package spaces

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/openfga/pkg/tuple"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"log/slog"

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
}

type spaceRecord struct {
	Space      habitat_syntax.SpaceURI `gorm:"primaryKey"`
	Owner      syntax.DID              `gorm:"primaryKey"`
	Collection syntax.NSID             `gorm:"primaryKey"`
	Rkey       syntax.RecordKey        `gorm:"primaryKey"`
	Value      []byte
	Rev        syntax.TID `gorm:"uniqueIndex"`
	Cid        string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// SpaceView is the public representation of a space
type SpaceView struct {
	URI         habitat_syntax.SpaceURI
	Type        syntax.NSID
	Skey        habitat_syntax.SpaceKey
	MemberCount int
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
	Cid        cid.Cid
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
		org syntax.DID,
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
	) error
	RemoveMember(ctx context.Context, space habitat_syntax.SpaceURI, did syntax.DID) error
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
	) (habitat_syntax.SpaceRecordURI, *cid.Cid, error)
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
	) error
	DeleteSpace(ctx context.Context, uri habitat_syntax.SpaceURI) error

	// Oplog operations
	GetRepoOplog(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		repo syntax.DID,
		since string,
		limit int,
	) ([]Record, error)
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
	if err := db.AutoMigrate(&space{}, &spaceRecord{}); err != nil {
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

func (s *store) CreateSpace(
	ctx context.Context,
	org syntax.DID,
	creator syntax.DID,
	spaceType syntax.NSID,
	skey habitat_syntax.SpaceKey,
) (habitat_syntax.SpaceURI, error) {
	tid := s.clock.Next()
	if skey == "" {
		skey = habitat_syntax.SpaceKey(tid)
	}

	err := s.db.Transaction(func(tx *gorm.DB) error {
		var existing space
		if err := tx.Where("owner = ? AND skey = ?", org, skey).First(&existing).Error; err == nil {
			return ErrSpaceAlreadyExists
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		if err := tx.Create(&space{
			Owner: org,
			Type:  spaceType,
			Skey:  skey,
			Rev:   tid,
		}).Error; err != nil {
			return err
		}

		return s.fga.Write(
			ctx,
			fgastore.MemberUserString(creator),
			fgastore.RelationSpaceOwner,
			fgastore.SpaceObjectKey(habitat_syntax.ConstructSpaceURI(org, spaceType, skey)),
		)
	})
	if err != nil {
		return "", err
	}

	return habitat_syntax.ConstructSpaceURI(org, spaceType, skey), nil
}

func (s *store) ListSpaces(
	ctx context.Context,
	member syntax.DID,
	filterOwner *syntax.DID,
	filterType *syntax.NSID,
) ([]SpaceView, error) {
	fgaObjects, err := s.fga.ListObjects(
		ctx,
		fgastore.MemberUserString(member),
		"can_read",
		"space",
	)
	if err != nil {
		return nil, fmt.Errorf("list fga spaces: %w", err)
	}
	if len(fgaObjects) == 0 {
		return []SpaceView{}, nil
	}

	conditions := s.db
	for _, key := range fgaObjects {
		uri, err := fgastore.ParseSpaceObjectKey(key)
		if err != nil {
			return nil, fmt.Errorf("parse space object key: %w", err)
		}
		conditions = conditions.Or("owner = ? AND skey = ?", uri.SpaceDID(), uri.Skey())
	}
	query := s.db.WithContext(ctx).Model(&space{}).Where(conditions)
	if filterOwner != nil {
		query = query.Where("owner = ?", *filterOwner)
	}
	if filterType != nil {
		query = query.Where("type = ?", *filterType)
	}

	var rows []space
	if err := query.Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}

	slog.DebugContext(ctx, "list spaces", "spaces", len(rows))

	views := make([]SpaceView, len(rows))
	for i, row := range rows {
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
	slog.DebugContext(ctx, "list spaces", "views", len(views))
	return views, nil
}

func (s *store) GetMembers(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
) ([]MemberInfo, error) {
	var sp space
	err := s.db.WithContext(ctx).
		Where("owner = ? AND skey = ?", uri.SpaceDID(), uri.Skey()).
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
) error {
	var sp space
	err := s.db.WithContext(ctx).
		Where("owner = ? AND skey = ?", uri.SpaceDID(), uri.Skey()).
		First(&sp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrSpaceNotFound
	} else if err != nil {
		return err
	}
	if did == uri.SpaceDID() {
		return nil
	}
	if access == SpaceAccessRead {
		return s.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
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
		})
	} else {
		return s.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
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
		})
	}
}

func (s *store) RemoveMember(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	did syntax.DID,
) error {
	if did == uri.SpaceDID() {
		return ErrCannotRemoveOwner
	}

	var sp space
	err := s.db.WithContext(ctx).
		Where("owner = ? AND skey = ?", uri.SpaceDID(), uri.Skey()).
		First(&sp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrSpaceNotFound
	} else if err != nil {
		return err
	}

	return s.fga.WriteRaw(ctx, &openfgav1.WriteRequest{
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
	})
}

// ---- Record operations ----

func (s *store) PutRecord(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	owner syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
	value map[string]any,
) (habitat_syntax.SpaceRecordURI, *cid.Cid, error) {
	var sp space
	err := s.db.WithContext(ctx).
		Where("owner = ? AND skey = ?", uri.SpaceDID(), uri.Skey()).
		First(&sp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil, ErrSpaceNotFound
	} else if err != nil {
		return "", nil, err
	}

	bytes, err := atdata.MarshalCBOR(value)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal record: %w", err)
	}

	cid, err := cid.V1Builder{}.WithCodec(cid.DagCBOR).Sum(bytes)
	if err != nil {
		return "", nil, fmt.Errorf("failed to compute cid: %w", err)
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
			Cid:        cid.String(),
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
		return "", nil, fmt.Errorf("failed to create record: %w", err)
	}
	return habitat_syntax.ConstructSpaceRecordURI(uri, owner, collection, rkey), &cid, nil
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
		Where("space = ? AND owner = ? AND collection = ? AND rkey = ?",
			uri, owner, collection, rkey).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrRecordNotFound
	} else if err != nil {
		return nil, err
	}

	value, err := atdata.UnmarshalCBOR(row.Value)
	if err != nil {
		return nil, err
	}

	return &Record{
		Collection: collection,
		Rkey:       row.Rkey,
		Value:      value,
		Rev:        string(row.Rev),
		UpdatedAt:  row.UpdatedAt,
		Cid:        cid.MustParse(row.Cid),
	}, nil
}

func (s *store) ListRecords(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	repo syntax.DID,
	collection *syntax.NSID,
) ([]Record, error) {
	query := s.db.WithContext(ctx).
		Where("space = ?", uri).
		Where("owner = ?", repo)

	if collection != nil {
		query = query.Where("collection = ?", collection)
	}

	var rows []spaceRecord
	if err := query.Order("rkey ASC").Find(&rows).Error; err != nil {
		return nil, err
	}

	records := make([]Record, len(rows))
	for i, row := range rows {
		value, err := atdata.UnmarshalCBOR(row.Value)
		if err != nil {
			return nil, err
		}
		records[i] = Record{
			Owner:      row.Owner,
			Collection: row.Collection,
			Rkey:       row.Rkey,
			Value:      value,
			Rev:        string(row.Rev),
			UpdatedAt:  row.UpdatedAt,
			Cid:        cid.MustParse(row.Cid),
		}
	}

	return records, nil
}

func (s *store) DeleteSpace(ctx context.Context, uri habitat_syntax.SpaceURI) error {
	// read the stored FGA tuples for this space before deleting anything,
	// so we know exactly what tuples to delete
	tuples, err := s.fga.Read(ctx, fgastore.Tuple{Object: fgastore.SpaceObjectKey(uri)})
	if err != nil {
		return err
	}

	// everything after this point is idempotent — use a transaction
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		deleteSpace := tx.
			Where("owner = ? AND skey = ?", uri.SpaceDID(), uri.Skey()).
			Delete(&space{})
		if deleteSpace.Error != nil {
			return err
		}
		if deleteSpace.RowsAffected == 0 {
			return ErrSpaceNotFound
		}

		if err := tx.
			Where("space = ?", uri).
			Delete(&spaceRecord{}).Error; err != nil {
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
}

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
		value, err := atdata.UnmarshalCBOR(row.Value)
		if err != nil {
			return nil, err
		}
		records[i] = Record{
			Owner:      row.Owner,
			Collection: row.Collection,
			Rkey:       row.Rkey,
			Value:      value,
			Rev:        string(row.Rev),
			UpdatedAt:  row.UpdatedAt,
			Cid:        cid.MustParse(row.Cid),
		}
	}
	return records, nil
}

func (s *store) DeleteRecord(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	collection syntax.NSID,
	rkey string,
) error {
	result := s.db.WithContext(ctx).
		Where("space = ? AND collection = ? AND rkey = ?",
			uri, collection, rkey).
		Delete(&spaceRecord{})

	if result.Error != nil {
		return result.Error
	}
	return nil
}
