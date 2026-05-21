package spaces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/habitat-network/habitat/internal/fgastore"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// GORM models

type space struct {
	Owner     syntax.DID          `gorm:"primaryKey"`
	Skey      habitat_syntax.SpaceKey `gorm:"primaryKey"`
	Type      syntax.NSID
	CreatedAt time.Time
}

type spaceRecord struct {
	Space      habitat_syntax.SpaceURI `gorm:"primaryKey"`
	Owner      syntax.DID              `gorm:"primaryKey"`
	Collection syntax.NSID             `gorm:"primaryKey"`
	Rkey       syntax.RecordKey        `gorm:"primaryKey"`
	Value      []byte
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
	AddedAt time.Time
}

// Record is a single record within a space
type Record struct {
	Collection syntax.NSID
	Rkey       string
	Value      map[string]any
	UpdatedAt  time.Time
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
	ListSpaces(
		ctx context.Context,
		owner syntax.DID,
		filterType *syntax.NSID,
	) ([]SpaceView, error)

	// Member operations
	AddMember(ctx context.Context, space habitat_syntax.SpaceURI, did syntax.DID) error
	RemoveMember(ctx context.Context, space habitat_syntax.SpaceURI, did syntax.DID) error
	GetMembers(ctx context.Context, space habitat_syntax.SpaceURI) ([]MemberInfo, error)
	IsMember(ctx context.Context, space habitat_syntax.SpaceURI, did syntax.DID) (bool, error)

	// Record operations
	PutRecord(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		owner syntax.DID,
		collection syntax.NSID,
		rkey string,
		value map[string]any,
	) error
	GetRecord(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		owner syntax.DID,
		collection syntax.NSID,
		rkey string,
	) (*Record, error)
	ListRecords(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		collection *syntax.NSID,
	) ([]Record, error)
	DeleteRecord(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		collection syntax.NSID,
		rkey string,
	) error
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
	db  *gorm.DB
	fga fgastore.Store
}

var _ Store = &store{}

func NewStore(db *gorm.DB, fga fgastore.Store) (*store, error) {
	if err := db.AutoMigrate(&space{}, &spaceRecord{}); err != nil {
		return nil, fmt.Errorf("failed to migrate spaces tables: %w", err)
	}
	return &store{db: db, fga: fga}, nil
}

// ownerContextualTuple returns a Tuple representing the owner relationship,
// for use as a contextual tuple in FGA queries.  This is how we make the
// owner a recognized member of the space without storing the owner in FGA.
func ownerContextualTuple(uri habitat_syntax.SpaceURI) fgastore.Tuple {
	return fgastore.Tuple{
		User:     fgastore.MemberUserString(uri.SpaceDID()),
		Relation: "owner",
		Object:   fgastore.SpaceObjectKey(uri),
	}
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
	}

	err = s.db.WithContext(ctx).Create(&sp).Error
	if err != nil {
		return "", err
	}

	return habitat_syntax.ConstructSpaceURI(owner, spaceType, skey), nil
}

func (s *store) ListSpaces(
	ctx context.Context,
	owner syntax.DID,
	filterType *syntax.NSID,
) ([]SpaceView, error) {
	var rows []space
	query := s.db.WithContext(ctx).Model(&space{}).
		Where("owner = ?", owner)
	if filterType != nil {
		query = query.Where("type = ?", filterType)
	}
	if err := query.Order("created_at DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	views := make([]SpaceView, len(rows))
	for i, row := range rows {
		views[i] = SpaceView{
			URI:  habitat_syntax.ConstructSpaceURI(row.Owner, row.Type, row.Skey),
			Type: row.Type,
			Skey: row.Skey,
		}
	}
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

	fgaUsers, err := s.fga.ListUsers(
		ctx,
		fgastore.SpaceObjectKey(uri),
		"member",
		ownerContextualTuple(uri),
	)
	if err != nil {
		return nil, fmt.Errorf("list fga members: %w", err)
	}

	members := make([]MemberInfo, len(fgaUsers))
	for i, u := range fgaUsers {
		did, err := fgastore.MemberUserToDID(u)
		if err != nil {
			continue
		}
		members[i] = MemberInfo{Did: did}
	}

	return members, nil
}

func (s *store) IsMember(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	did syntax.DID,
) (bool, error) {
	var sp space
	err := s.db.WithContext(ctx).
		Where("owner = ? AND skey = ?", uri.SpaceDID(), uri.Skey()).
		First(&sp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}

	return s.fga.Check(
		ctx,
		fgastore.MemberUserString(did),
		"member",
		fgastore.SpaceObjectKey(uri),
		ownerContextualTuple(uri),
	)
}

func (s *store) AddMember(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	did syntax.DID,
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

	exists, err := s.fga.Check(
		ctx,
		fgastore.MemberUserString(did),
		"member",
		fgastore.SpaceObjectKey(uri),
		ownerContextualTuple(uri),
	)
	if err != nil {
		return err
	}
	if exists {
		return ErrUserAlreadyMember
	}

	return s.fga.Write(ctx, fgastore.MemberUserString(did), "member", fgastore.SpaceObjectKey(uri))
}

func (s *store) RemoveMember(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	did syntax.DID,
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
		return ErrCannotRemoveOwner
	}

	exists, err := s.fga.Check(
		ctx,
		fgastore.MemberUserString(did),
		"member",
		fgastore.SpaceObjectKey(uri),
		ownerContextualTuple(uri),
	)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotAMember
	}

	return s.fga.Delete(ctx, fgastore.MemberUserString(did), "member", fgastore.SpaceObjectKey(uri))
}

// ---- Record operations ----

func (s *store) PutRecord(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	owner syntax.DID,
	collection syntax.NSID,
	rkey string,
	value map[string]any,
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

	bytes, err := json.Marshal(value)
	if err != nil {
		return err
	}

	rec := spaceRecord{
		Owner:      owner,
		Space:      uri,
		Collection: collection,
		Rkey:       rkey,
		Value:      bytes,
	}

	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{UpdateAll: true}).
		Create(&rec).Error
}

func (s *store) GetRecord(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	owner syntax.DID,
	collection syntax.NSID,
	rkey string,
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

	var value map[string]any
	if err := json.Unmarshal(row.Value, &value); err != nil {
		return nil, err
	}

	return &Record{
		Collection: collection,
		Rkey:       row.Rkey,
		Value:      value,
		UpdatedAt:  row.UpdatedAt,
	}, nil
}

func (s *store) ListRecords(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	collection *syntax.NSID,
) ([]Record, error) {
	query := s.db.WithContext(ctx).
		Where("space = ?", uri)

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
			Collection: row.Collection,
			Rkey:       row.Rkey,
			Value:      value,
			UpdatedAt:  row.UpdatedAt,
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
