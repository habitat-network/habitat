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

// recordRow is one indexed record synced from a member's repo, keyed by its
// full space-record URI (spaceURI/repo/collection/rkey). The same underlying
// atproto record (repo+collection+rkey) can appear in several spaces, giving
// one row per (space, record). The record body is not stored; the collections
// endpoints only expose its identity and which spaces it belongs to.
type recordRow struct {
	RecordURI  string `gorm:"column:record_uri;primaryKey"`
	SpaceURI   string `gorm:"column:space_uri;index"`
	Repo       string `gorm:"column:repo"`
	Collection string `gorm:"column:collection;index"`
	Rkey       string `gorm:"column:rkey"`
	// AtURI is the collection-scoped atproto URI (at://repo/collection/rkey),
	// the identity shared by the same record across spaces.
	AtURI     string    `gorm:"column:at_uri;index"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (recordRow) TableName() string { return "records" }

// collectionCount is one row of the per-collection record count, counting
// distinct atproto records (not per-space copies).
type collectionCount struct {
	Collection string `gorm:"column:collection"`
	Count      int64  `gorm:"column:count"`
}

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
	if err := db.AutoMigrate(&groupRow{}, &tupleRow{}, &recordRow{}, &orgSessionRow{}); err != nil {
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

// UpsertRecord indexes a synced record by its space-record URI. URIs that do
// not parse into all four parts (space, repo, collection, rkey) are ignored so
// a malformed message does not wedge the indexer.
func (s *Store) UpsertRecord(ctx context.Context, uri habitat_syntax.SpaceRecordURI) error {
	space := uri.SpaceURI()
	repo := uri.Repo()
	collection := uri.Collection()
	rkey := uri.Rkey()
	if space == "" || repo == "" || collection == "" || rkey == "" {
		return nil
	}
	row := recordRow{
		RecordURI:  uri.String(),
		SpaceURI:   space.String(),
		Repo:       repo.String(),
		Collection: collection.String(),
		Rkey:       rkey.String(),
		AtURI:      "at://" + repo.String() + "/" + collection.String() + "/" + rkey.String(),
		UpdatedAt:  time.Now(),
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "record_uri"}},
		UpdateAll: true,
	}).Create(&row).Error
}

func (s *Store) DeleteRecord(ctx context.Context, uri habitat_syntax.SpaceRecordURI) error {
	return s.db.WithContext(ctx).
		Where("record_uri = ?", uri.String()).
		Delete(&recordRow{}).Error
}

// CountCollections returns, for each collection with at least one record in the
// given spaces, the number of records in that collection, counting a record
// once per space it belongs to (each space holds its own version). Returns an
// empty slice when spaces is empty.
func (s *Store) CountCollections(
	ctx context.Context,
	spaces []string,
) ([]collectionCount, error) {
	if len(spaces) == 0 {
		return []collectionCount{}, nil
	}
	var counts []collectionCount
	err := s.db.WithContext(ctx).
		Model(&recordRow{}).
		Select("collection, COUNT(*) AS count").
		Where("space_uri IN ?", spaces).
		Group("collection").
		Order("collection ASC").
		Scan(&counts).Error
	if err != nil {
		return nil, err
	}
	return counts, nil
}

// ListRecordsInSpaces returns every indexed record row in the given collection
// that belongs to one of the given spaces, one row per space-record. Returns
// nil when spaces is empty.
func (s *Store) ListRecordsInSpaces(
	ctx context.Context,
	spaces []string,
	collection string,
) ([]recordRow, error) {
	if len(spaces) == 0 {
		return nil, nil
	}
	var rows []recordRow
	err := s.db.WithContext(ctx).
		Where("collection = ? AND space_uri IN ?", collection, spaces).
		Order("at_uri ASC, space_uri ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
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

// FindUserTuples returns the tuples granting did any role directly on space.
func (s *Store) FindUserTuples(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
) ([]tupleRow, error) {
	var rows []tupleRow
	err := s.db.WithContext(ctx).
		Where(
			"object_space = ? AND subject_kind = ? AND subject_did = ?",
			space.String(), "user", did.String(),
		).
		Find(&rows).Error
	return rows, err
}

// FindGroupTuples returns the tuples making space inherit subjectGroup's members.
func (s *Store) FindGroupTuples(
	ctx context.Context,
	space, subjectGroup habitat_syntax.SpaceURI,
) ([]tupleRow, error) {
	var rows []tupleRow
	err := s.db.WithContext(ctx).
		Where(
			"object_space = ? AND subject_kind = ? AND subject_group = ?",
			space.String(), "group", subjectGroup.String(),
		).
		Find(&rows).Error
	return rows, err
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

// OrgDIDs returns every org currently registered (via
// network.habitat.org.requestCrawl).
func (s *Store) OrgDIDs(ctx context.Context) ([]syntax.DID, error) {
	var rows []orgSessionRow
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}
	dids := make([]syntax.DID, len(rows))
	for i, row := range rows {
		dids[i] = syntax.DID(row.DID)
	}
	return dids, nil
}

// OrgSessionID returns the session id saved for orgDID specifically. Home can
// be registered (via network.habitat.org.requestCrawl) for several orgs at
// once, each with its own credential, so callers with a target space must
// look up by that space's owning org rather than any single "current" org.
func (s *Store) OrgSessionID(ctx context.Context, orgDID syntax.DID) (string, error) {
	var row orgSessionRow
	err := s.db.WithContext(ctx).Where("did = ?", orgDID.String()).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrNotAuthorized
	}
	if err != nil {
		return "", err
	}
	return row.SessionID, nil
}

// MostRecentOrgSession returns the most recently registered org's DID and
// session id. Used only where there is no target space to derive the owning
// org from (e.g. creating a brand new group) — with more than one org
// registered this is ambiguous, so prefer OrgSessionID wherever a space is
// available.
func (s *Store) MostRecentOrgSession(ctx context.Context) (syntax.DID, string, error) {
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
	"home server is not authorized for this org; complete network.habitat.org.requestCrawl",
)
