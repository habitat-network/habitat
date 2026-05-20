package spaces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SpaceURI identifies a space.
// Format: "habitat://spaceDID/spaceType/skey"
// See https://dholms.leaflet.pub/3mlegohgtps2k
type SpaceURI string

var spaceURIRegex = regexp.MustCompile(
	`^habitat:\/\/(?P<did>[a-zA-Z0-9._:%-]+)\/(?P<type>[a-zA-Z0-9-.]+)\/(?P<skey>[a-zA-Z0-9_~.:-]{1,512})$`,
)

func ConstructSpaceURI(spaceDID syntax.DID, spaceType syntax.NSID, skey string) SpaceURI {
	return SpaceURI(fmt.Sprintf("habitat://%s/%s/%s", spaceDID, spaceType, skey))
}

func ParseSpaceURI(raw string) (SpaceURI, error) {
	if len(raw) > 8192 {
		return "", errors.New("SpaceURI is too long (8192 chars max)")
	}
	parts := spaceURIRegex.FindStringSubmatch(raw)
	if len(parts) < 4 || parts[0] == "" {
		return "", errors.New("invalid space URI format")
	}
	_, err := syntax.ParseDID(parts[1])
	if err != nil {
		return "", fmt.Errorf("space URI DID is not valid: %s", parts[1])
	}
	_, err = syntax.ParseNSID(parts[2])
	if err != nil {
		return "", fmt.Errorf("space URI type is not a valid NSID: %s", parts[2])
	}
	return SpaceURI(raw), nil
}

func (s SpaceURI) SpaceDID() syntax.DID {
	parts := spaceURIRegex.FindStringSubmatch(string(s))
	if len(parts) < 4 {
		return ""
	}
	did, err := syntax.ParseDID(parts[1])
	if err != nil {
		return ""
	}
	return did
}

func (s SpaceURI) SpaceType() syntax.NSID {
	parts := spaceURIRegex.FindStringSubmatch(string(s))
	if len(parts) < 4 {
		return ""
	}
	nsid, err := syntax.ParseNSID(parts[2])
	if err != nil {
		return ""
	}
	return nsid
}

func (s SpaceURI) Skey() string {
	parts := spaceURIRegex.FindStringSubmatch(string(s))
	if len(parts) < 4 {
		return ""
	}
	return parts[3]
}

func (s SpaceURI) String() string {
	return string(s)
}

// GORM models

type space struct {
	Owner     string `gorm:"primaryKey"`
	Skey      string `gorm:"primaryKey"`
	Type      string `gorm:"index"`
	CreatedAt time.Time
}

// TODO: members table will be added when the permission store is built.
// For now, the owner is always the sole member of a space.

type spaceRecord struct {
	Owner      string `gorm:"primaryKey"`
	Skey       string `gorm:"primaryKey"`
	Collection string `gorm:"primaryKey"`
	Rkey       string `gorm:"primaryKey"`
	Value      []byte
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// SpaceView is the public representation of a space
type SpaceView struct {
	URI         SpaceURI
	Type        syntax.NSID
	Skey        string
	MemberCount int
}

// MemberInfo holds a member's DID and when they were added
type MemberInfo struct {
	Did     syntax.DID
	AddedAt time.Time
}

// RecordInSpace is a single record within a space
type RecordInSpace struct {
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
		skey string,
	) (SpaceURI, error)
	ListSpaces(ctx context.Context, actor syntax.DID, filterType *syntax.NSID, filterOwner *syntax.DID) ([]SpaceView, error)

	// Member operations
	// TODO: AddMember and RemoveMember will be added when the permission store is built.
	GetMembers(ctx context.Context, space SpaceURI) ([]MemberInfo, error)
	IsMember(ctx context.Context, space SpaceURI, did syntax.DID) (bool, error)

	// Record operations
	PutRecord(ctx context.Context, space SpaceURI, collection syntax.NSID, rkey string, value map[string]any) error
	GetRecord(ctx context.Context, space SpaceURI, collection syntax.NSID, rkey string) (*RecordInSpace, error)
	ListRecords(ctx context.Context, space SpaceURI, collection *syntax.NSID) ([]RecordInSpace, error)
	DeleteRecord(ctx context.Context, space SpaceURI, collection syntax.NSID, rkey string) error
}

var (
	ErrSpaceNotFound      = errors.New("space not found")
	ErrSpaceAlreadyExists = errors.New("space already exists")
	ErrRecordNotFound     = errors.New("record not found")
)

// ---- Store implementation ----

type store struct {
	db *gorm.DB
}

var _ Store = &store{}

func NewStore(db *gorm.DB) (*store, error) {
	if err := db.AutoMigrate(&space{}, &spaceRecord{}); err != nil {
		return nil, fmt.Errorf("failed to migrate spaces tables: %w", err)
	}
	return &store{db: db}, nil
}

func (s *store) CreateSpace(
	ctx context.Context,
	owner syntax.DID,
	spaceType syntax.NSID,
	skey string,
) (SpaceURI, error) {
	if skey == "" {
		skey = uuid.New().String()
	}

	var existing space
	err := s.db.WithContext(ctx).Where("owner = ? AND skey = ?", owner.String(), skey).First(&existing).Error
	if err == nil {
		return "", ErrSpaceAlreadyExists
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}

	sp := space{
		Owner: owner.String(),
		Skey:  skey,
		Type:  spaceType.String(),
	}

	err = s.db.WithContext(ctx).Create(&sp).Error
	if err != nil {
		return "", err
	}

	return ConstructSpaceURI(owner, spaceType, skey), nil
}

func (s *store) ListSpaces(
	ctx context.Context,
	actor syntax.DID,
	filterType *syntax.NSID,
	filterOwner *syntax.DID,
) ([]SpaceView, error) {
	query := s.db.WithContext(ctx).Model(&space{})

	// TODO: When the permission store is built, this will query across
	// memberships. For now only the owner sees their own spaces.
	if filterOwner != nil {
		query = query.Where("owner = ?", filterOwner.String())
	} else {
		query = query.Where("owner = ?", actor.String())
	}

	if filterType != nil {
		query = query.Where("type = ?", filterType.String())
	}

	var rows []space
	if err := query.Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}

	views := make([]SpaceView, len(rows))
	for i, row := range rows {
		nsid, _ := syntax.ParseNSID(row.Type)
		views[i] = SpaceView{
			URI:         ConstructSpaceURI(syntax.DID(row.Owner), nsid, row.Skey),
			Type:        nsid,
			Skey:        row.Skey,
			MemberCount: 1, // only the owner for now
		}
	}

	return views, nil
}

func (s *store) GetMembers(
	ctx context.Context,
	uri SpaceURI,
) ([]MemberInfo, error) {
	var sp space
	err := s.db.WithContext(ctx).
		Where("owner = ? AND skey = ?", uri.SpaceDID().String(), uri.Skey()).
		First(&sp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSpaceNotFound
	} else if err != nil {
		return nil, err
	}

	return []MemberInfo{
		{Did: uri.SpaceDID(), AddedAt: sp.CreatedAt},
	}, nil
}

func (s *store) IsMember(
	ctx context.Context,
	uri SpaceURI,
	did syntax.DID,
) (bool, error) {
	var sp space
	err := s.db.WithContext(ctx).
		Where("owner = ? AND skey = ?", uri.SpaceDID().String(), uri.Skey()).
		First(&sp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}

	return did == uri.SpaceDID(), nil
}

// ---- Record operations ----

func (s *store) PutRecord(
	ctx context.Context,
	uri SpaceURI,
	collection syntax.NSID,
	rkey string,
	value map[string]any,
) error {
	owner := uri.SpaceDID().String()
	skey := uri.Skey()

	bytes, err := json.Marshal(value)
	if err != nil {
		return err
	}

	rec := spaceRecord{
		Owner:      owner,
		Skey:       skey,
		Collection: collection.String(),
		Rkey:       rkey,
		Value:      bytes,
	}

	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{UpdateAll: true}).
		Create(&rec).Error
}

func (s *store) GetRecord(
	ctx context.Context,
	uri SpaceURI,
	collection syntax.NSID,
	rkey string,
) (*RecordInSpace, error) {
	owner := uri.SpaceDID().String()
	skey := uri.Skey()

	var row spaceRecord
	err := s.db.WithContext(ctx).
		Where("owner = ? AND skey = ? AND collection = ? AND rkey = ?",
			owner, skey, collection.String(), rkey).
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

	return &RecordInSpace{
		Collection: collection,
		Rkey:       row.Rkey,
		Value:      value,
		UpdatedAt:  row.UpdatedAt,
	}, nil
}

func (s *store) ListRecords(
	ctx context.Context,
	uri SpaceURI,
	collection *syntax.NSID,
) ([]RecordInSpace, error) {
	owner := uri.SpaceDID().String()
	skey := uri.Skey()

	query := s.db.WithContext(ctx).
		Where("owner = ? AND skey = ?", owner, skey)

	if collection != nil {
		query = query.Where("collection = ?", collection.String())
	}

	var rows []spaceRecord
	if err := query.Order("rkey ASC").Find(&rows).Error; err != nil {
		return nil, err
	}

	records := make([]RecordInSpace, len(rows))
	for i, row := range rows {
		var value map[string]any
		if err := json.Unmarshal(row.Value, &value); err != nil {
			return nil, err
		}
		nsid, _ := syntax.ParseNSID(row.Collection)
		records[i] = RecordInSpace{
			Collection: nsid,
			Rkey:       row.Rkey,
			Value:      value,
			UpdatedAt:  row.UpdatedAt,
		}
	}

	return records, nil
}

func (s *store) DeleteRecord(
	ctx context.Context,
	uri SpaceURI,
	collection syntax.NSID,
	rkey string,
) error {
	owner := uri.SpaceDID().String()
	skey := uri.Skey()

	result := s.db.WithContext(ctx).
		Where("owner = ? AND skey = ? AND collection = ? AND rkey = ?",
			owner, skey, collection.String(), rkey).
		Delete(&spaceRecord{})

	if result.Error != nil {
		return result.Error
	}
	return nil
}
