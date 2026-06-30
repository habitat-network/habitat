package main

import (
	"context"
	"errors"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrGroupNotFound is returned by GetGroup when no profile is indexed for a
// space URI.
var ErrGroupNotFound = errors.New("group not found")

// groupRow is one indexed group profile, keyed by the group-space URI (the
// network.habitat.group.profile self record is unique per space).
type groupRow struct {
	SpaceURI    string `gorm:"column:space_uri;primaryKey"`
	Name        string `gorm:"column:name"`
	Description string `gorm:"column:description"`
	CreatedAt   string `gorm:"column:created_at"`
	// RecordURI is the full record URI of the profile, kept so deletes that
	// arrive keyed by record URI can find the row.
	RecordURI string    `gorm:"column:record_uri;index"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (groupRow) TableName() string { return "group_profiles" }

// tupleRow is one indexed relationship tuple granting a role on a group-space.
// Keyed by the tuple record URI so updates and deletes are idempotent.
type tupleRow struct {
	RecordURI string `gorm:"column:record_uri;primaryKey"`
	// ObjectSpace is the group-space the role is granted on.
	ObjectSpace string `gorm:"column:object_space;index"`
	Relation    string `gorm:"column:relation"`
	// SubjectKind is "user" or "group".
	SubjectKind string `gorm:"column:subject_kind"`
	// SubjectDID is set when SubjectKind == "user".
	SubjectDID string `gorm:"column:subject_did"`
	// SubjectGroup / SubjectRole are set when SubjectKind == "group" (a
	// space-role userset, i.e. an inherited group).
	SubjectGroup string `gorm:"column:subject_group"`
	SubjectRole  string `gorm:"column:subject_role"`
}

func (tupleRow) TableName() string { return "group_tuples" }

// orgSessionRow records the OAuth session id obtained when the home server is
// authorized for an org, so the org credential can be rebuilt after a restart.
type orgSessionRow struct {
	DID       string    `gorm:"column:did;primaryKey"`
	SessionID string    `gorm:"column:session_id"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (orgSessionRow) TableName() string { return "org_sessions" }

// Store is the home server's index of group profiles and their membership
// tuples, populated from sap's outbox and queried by the groups endpoints.
type Store struct {
	db *gorm.DB
}

func NewStore(db *gorm.DB) (*Store, error) {
	if err := db.AutoMigrate(&groupRow{}, &tupleRow{}, &orgSessionRow{}); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) UpsertProfile(
	ctx context.Context,
	recordURI habitat_syntax.SpaceRecordURI,
	name, description, createdAt string,
) error {
	row := groupRow{
		SpaceURI:    recordURI.SpaceURI().String(),
		Name:        name,
		Description: description,
		CreatedAt:   createdAt,
		RecordURI:   recordURI.String(),
		UpdatedAt:   time.Now(),
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "space_uri"}},
		UpdateAll: true,
	}).Create(&row).Error
}

func (s *Store) DeleteProfile(ctx context.Context, recordURI habitat_syntax.SpaceRecordURI) error {
	return s.db.WithContext(ctx).
		Where("record_uri = ?", recordURI.String()).
		Delete(&groupRow{}).Error
}

func (s *Store) UpsertTuple(ctx context.Context, t tupleRow) error {
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "record_uri"}},
		UpdateAll: true,
	}).Create(&t).Error
}

func (s *Store) DeleteTuple(ctx context.Context, recordURI habitat_syntax.SpaceRecordURI) error {
	return s.db.WithContext(ctx).
		Where("record_uri = ?", recordURI.String()).
		Delete(&tupleRow{}).Error
}

func (s *Store) ListGroups(ctx context.Context) ([]groupRow, error) {
	var rows []groupRow
	if err := s.db.WithContext(ctx).Order("name ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Store) GetGroup(ctx context.Context, space habitat_syntax.SpaceURI) (groupRow, error) {
	var row groupRow
	err := s.db.WithContext(ctx).Where("space_uri = ?", space.String()).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return groupRow{}, ErrGroupNotFound
	}
	return row, err
}

func (s *Store) ListTuples(ctx context.Context) ([]tupleRow, error) {
	var rows []tupleRow
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Store) SaveOrgSession(ctx context.Context, did syntax.DID, sessionID string) error {
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "did"}},
		UpdateAll: true,
	}).Create(&orgSessionRow{
		DID:       did.String(),
		SessionID: sessionID,
		UpdatedAt: time.Now(),
	}).Error
}

// OrgSession returns the single managed org's DID and session id. The home
// server manages exactly one org, so it returns the most recently saved one.
func (s *Store) OrgSession(ctx context.Context) (syntax.DID, string, error) {
	var row orgSessionRow
	err := s.db.WithContext(ctx).Order("updated_at DESC").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", "", ErrNotAuthorized
	}
	if err != nil {
		return "", "", err
	}
	return syntax.DID(row.DID), row.SessionID, nil
}

// ErrNotAuthorized indicates the home server has not yet completed the org
// OAuth bootstrap, so it holds no org credential.
var ErrNotAuthorized = errors.New(
	"home server is not authorized for any org; complete /oauth/login",
)
