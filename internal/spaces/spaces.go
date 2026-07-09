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

	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/events"
	"github.com/habitat-network/habitat/internal/fgastore"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// GORM models

type space struct {
	Owner     syntax.DID              `gorm:"primaryKey"`
	Type      syntax.NSID             `gorm:"primaryKey"`
	Skey      habitat_syntax.SpaceKey `gorm:"primaryKey"`
	CreatedAt time.Time
}

type spaceRecord struct {
	Space      habitat_syntax.SpaceURI `gorm:"primaryKey"`
	Repo       syntax.DID              `gorm:"primaryKey"`
	Collection syntax.NSID             `gorm:"primaryKey"`
	Rkey       syntax.RecordKey        `gorm:"primaryKey"`
	Value      []byte
	Rev        syntax.TID `gorm:"uniqueIndex"`
	Cid        string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// repoHashState caches a permissioned repo's LtHash so reads (listRepos,
// listRepoOps commit) don't rescan every record. State is the 2048-byte LtHash
// buffer, maintained incrementally in the write path (folded in on put, out on
// delete). Rev tracks the repo's latest write revision.
type repoHashState struct {
	Space     habitat_syntax.SpaceURI `gorm:"primaryKey"`
	Repo      syntax.DID              `gorm:"primaryKey"`
	State     []byte
	Rev       syntax.TID
	UpdatedAt time.Time
}

// SpaceView is the public representation of a space
type SpaceView struct {
	URI         habitat_syntax.SpaceURI
	Type        syntax.NSID
	Skey        habitat_syntax.SpaceKey
	MemberCount int
}

// RepoInfo holds a repo's DID and latest rev within a space
type RepoInfo struct {
	DID  syntax.DID
	Rev  string
	Hash []byte
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
		org syntax.DID,
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
	ListRepos(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
	) ([]RepoInfo, error)
	IsMember(
		ctx context.Context,
		org syntax.DID,
		space habitat_syntax.SpaceURI,
		did syntax.DID,
	) (bool, error)

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
		repo syntax.DID,
		collection syntax.NSID,
		rkey string,
	) error
	DeleteSpace(ctx context.Context, uri habitat_syntax.SpaceURI) error

	// Oplog operations
	//
	// ListRepoOps returns a repo's operations within a space after a given
	// revision, ordered by revision ascending, for incremental sync.
	ListRepoOps(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		repo syntax.DID,
		since string,
		limit int,
	) ([]Record, error)

	// RepoHead returns a repo's current head revision and LtHash commit hash.
	// found is false when the repo holds no records in the space.
	RepoHead(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		repo syntax.DID,
	) (rev string, hash []byte, found bool, err error)

	// WithTx returns a copy of the store scoped to the given transaction, so its
	// DB writes participate in a caller-managed transaction. FGA writes are not
	// transactional with the DB, but callers run them inside the same closure so
	// a DB rollback follows an FGA failure.
	db.Store[Store]
}

var (
	ErrSpaceNotFound      = errors.New("space not found")
	ErrSpaceAlreadyExists = errors.New("space already exists")
	ErrRecordNotFound     = errors.New("record not found")
	ErrUserAlreadyMember  = errors.New("user is already a member of the space")
	ErrNotAMember         = errors.New("user is not a member of the space")
	ErrCannotRemoveOrg    = errors.New("cannot remove the org from the space")
)

// ---- Store implementation ----

type store struct {
	db         *gorm.DB
	fga        fgastore.Store
	clock      *syntax.TIDClock
	eventStore events.Store
}

var _ Store = &store{}

func NewStore(db *gorm.DB, fga fgastore.Store, eventStore events.Store) (*store, error) {
	if err := db.AutoMigrate(&space{}, &spaceRecord{}, &repoHashState{}); err != nil {
		return nil, fmt.Errorf("failed to migrate spaces tables: %w", err)
	}
	return &store{db: db, fga: fga, clock: syntax.NewTIDClock(0), eventStore: eventStore}, nil
}

// WithTx implements [Store], returning a store whose DB operations run on tx.
func (s *store) WithTx(tx *gorm.DB) Store {
	return &store{
		db:         tx,
		fga:        s.fga,
		clock:      s.clock,
		eventStore: s.eventStore,
	}
}

// ownerContextualTuple returns a Tuple representing the owner relationship,
// for use as a contextual tuple in FGA queries.  This is how we make the
// owner a recognized member of the space without storing the owner in FGA.
func ownerContextualTuple(uri habitat_syntax.SpaceURI) fgastore.Tuple {
	return fgastore.Tuple{
		User:     fgastore.MemberUserString(uri.SpaceOwner()),
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
	if skey == "" {
		skey = habitat_syntax.NewSkey(s.clock.Next())
	}

	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&space{
			Owner: org,
			Type:  spaceType,
			Skey:  skey,
		}).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return ErrSpaceAlreadyExists
			}
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
	org syntax.DID,
	member syntax.DID,
	filterOwner *syntax.DID,
	filterType *syntax.NSID,
) ([]SpaceView, error) {
	var conditions *gorm.DB
	if member == "" {
		conditions = s.db
	} else {
		// If member is the org itself, include all the org's spaces. This initial condition
		// is always false for user members since they are never owners of a space.
		conditions = s.db.Where("owner = ?", member)
		fgaObjects, err := s.fga.ListObjects(
			ctx,
			fgastore.MemberUserString(member),
			"can_read",
			"space",
			fgastore.OrgMemberContextualTuple(org),
		)
		if err != nil {
			return nil, fmt.Errorf("list fga spaces: %w", err)
		}
		for _, key := range fgaObjects {
			uri, err := fgastore.ParseSpaceObjectKey(key)
			if err != nil {
				return nil, fmt.Errorf("parse space object key: %w", err)
			}
			conditions = conditions.Or("owner = ? AND skey = ?", uri.SpaceOwner(), uri.Skey())
		}
	}

	query := s.db.WithContext(ctx).Model(&space{}).Where(conditions).Distinct()
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

	views := make([]SpaceView, len(rows))
	for i, row := range rows {
		uri := habitat_syntax.ConstructSpaceURI(row.Owner, row.Type, row.Skey)
		fgaUsers, err := s.fga.ListUsers(
			ctx,
			fgastore.SpaceObjectKey(uri),
			"can_read",
			ownerContextualTuple(uri),
			fgastore.OrgMemberContextualTuple(org),
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
	return views, nil
}

// loadRepoHash reads a repo's cached LtHash state and rev, or the zero hash and
// empty rev (found=false) when no row exists yet.
func loadRepoHash(
	tx *gorm.DB,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
) (ltHash, syntax.TID, bool, error) {
	var row repoHashState
	err := tx.Where("space = ? AND repo = ?", space, repo).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ltHash{}, "", false, nil
	}
	if err != nil {
		return ltHash{}, "", false, err
	}
	return loadLtHash(row.State), row.Rev, true, nil
}

// saveRepoHash persists a repo's LtHash state and rev.
func saveRepoHash(
	tx *gorm.DB,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	h ltHash,
	rev syntax.TID,
) error {
	return tx.Save(&repoHashState{
		Space: space,
		Repo:  repo,
		State: h.state(),
		Rev:   rev,
	}).Error
}

func (s *store) ListRepos(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
) ([]RepoInfo, error) {
	var sp space
	err := s.db.WithContext(ctx).
		Where("owner = ? AND skey = ?", uri.SpaceOwner(), uri.Skey()).
		First(&sp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSpaceNotFound
	} else if err != nil {
		return nil, err
	}

	// The writer set and each repo's hash come straight from the cached hash
	// table, maintained incrementally by the write path — no record rescan.
	var rows []repoHashState
	if err := s.db.WithContext(ctx).
		Where("space = ?", uri).
		Order("repo ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	repos := make([]RepoInfo, len(rows))
	for i, row := range rows {
		h := loadLtHash(row.State)
		repos[i] = RepoInfo{
			DID:  row.Repo,
			Rev:  string(row.Rev),
			Hash: h.sum(),
		}
	}
	return repos, nil
}

func (s *store) IsMember(
	ctx context.Context,
	org syntax.DID,
	uri habitat_syntax.SpaceURI,
	did syntax.DID,
) (bool, error) {
	return s.fga.Check(
		ctx,
		fgastore.MemberUserString(did),
		fgastore.RelationSpaceReader,
		fgastore.SpaceObjectKey(uri),
		ownerContextualTuple(uri),
		fgastore.OrgMemberContextualTuple(org),
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
		Where("owner = ? AND skey = ?", uri.SpaceOwner(), uri.Skey()).
		First(&sp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrSpaceNotFound
	} else if err != nil {
		return err
	}
	if did == uri.SpaceOwner() {
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
	if did == uri.SpaceOwner() {
		return ErrCannotRemoveOrg
	}

	var sp space
	err := s.db.WithContext(ctx).
		Where("owner = ? AND skey = ?", uri.SpaceOwner(), uri.Skey()).
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
	spaceUri habitat_syntax.SpaceURI,
	repo syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
	value map[string]any,
) (habitat_syntax.SpaceRecordURI, *cid.Cid, error) {
	var sp space
	err := s.db.WithContext(ctx).
		Where("owner = ?", spaceUri.SpaceOwner()).
		Where("type = ?", spaceUri.SpaceType()).
		Where("skey = ?", spaceUri.Skey()).
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

	var recordUri habitat_syntax.SpaceRecordURI
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if tx.Name() == "postgres" {
			// acquire lock on permissioned repo within space
			if err := tx.Exec(
				`SELECT pg_advisory_xact_lock(hashtext(?), hashtext(?))`,
				spaceUri,
				repo,
			).Error; err != nil {
				return err
			}
		}
		action := "update"
		var prev spaceRecord
		err := tx.
			Where("space = ?", spaceUri).
			Where("repo = ?", repo).
			Order("rev DESC").
			First(&prev).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			action = "create"
		} else if err != nil {
			return err
		}
		tid := s.clock.Next()
		if rkey == "" {
			rkey = syntax.RecordKey(tid)
		}
		recordUri = habitat_syntax.ConstructSpaceRecordURI(spaceUri, repo, collection, rkey)
		if err := s.eventStore.WithTx(tx).AppendSpaceEvent(
			ctx,
			spaceUri,
			repo,
			tid,
			prev.Rev,
			[]events.EventOps{
				{
					Action: action,
					Uri:    recordUri,
					Value:  value,
					Cid:    cid.String(),
				},
			},
		); err != nil {
			return err
		}

		// Maintain the cached LtHash: fold out this record's previous element (if
		// it already existed) and fold in the new one, then advance the rev.
		var existing spaceRecord
		existErr := tx.
			Where("space = ? AND repo = ? AND collection = ? AND rkey = ?",
				spaceUri, repo, collection, rkey).
			First(&existing).Error
		if existErr != nil && !errors.Is(existErr, gorm.ErrRecordNotFound) {
			return existErr
		}
		h, _, _, err := loadRepoHash(tx, spaceUri, repo)
		if err != nil {
			return err
		}
		if existErr == nil {
			h.remove(recordElement(collection.String(), rkey.String(), existing.Cid))
		}
		h.add(recordElement(collection.String(), rkey.String(), cid.String()))
		if err := saveRepoHash(tx, spaceUri, repo, h, tid); err != nil {
			return err
		}

		return tx.Save(&spaceRecord{
			Repo:       repo,
			Space:      spaceUri,
			Collection: collection,
			Rkey:       rkey,
			Value:      bytes,
			Rev:        tid,
			Cid:        cid.String(),
		}).Error
	})
	if err != nil {
		return "", nil, fmt.Errorf("failed to create record: %w", err)
	}
	s.eventStore.NotifyEvent(ctx)
	return recordUri, &cid, nil
}

func (s *store) GetRecord(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	repo syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
) (*Record, error) {
	var row spaceRecord
	err := s.db.WithContext(ctx).
		Where("space = ? AND repo = ? AND collection = ? AND rkey = ?",
			uri, repo, collection, rkey).
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
		Where("repo = ?", repo)

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
			Owner:      row.Repo,
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
			Where("owner = ? AND skey = ?", uri.SpaceOwner(), uri.Skey()).
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

func (s *store) ListRepoOps(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	repo syntax.DID,
	since string,
	limit int,
) ([]Record, error) {
	query := s.db.WithContext(ctx).
		Model(&spaceRecord{}).
		Where("space = ? AND repo = ?", uri, repo)

	if since != "" {
		query = query.Where("rev > ?", since)
	}

	if limit <= 0 {
		limit = 100
	}

	var rows []spaceRecord
	if err := query.Order("rev ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list repo ops: %w", err)
	}

	records := make([]Record, len(rows))
	for i, row := range rows {
		value, err := atdata.UnmarshalCBOR(row.Value)
		if err != nil {
			return nil, err
		}
		records[i] = Record{
			Owner:      row.Repo,
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

func (s *store) RepoHead(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	repo syntax.DID,
) (string, []byte, bool, error) {
	h, rev, found, err := loadRepoHash(s.db.WithContext(ctx), uri, repo)
	if err != nil {
		return "", nil, false, fmt.Errorf("repo head: %w", err)
	}
	if !found {
		return "", nil, false, nil
	}
	return string(rev), h.sum(), true, nil
}

func (s *store) DeleteRecord(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	repo syntax.DID,
	collection syntax.NSID,
	rkey string,
) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if tx.Name() == "postgres" {
			if err := tx.Exec(
				`SELECT pg_advisory_xact_lock(hashtext(?), hashtext(?))`,
				uri,
				repo,
			).Error; err != nil {
				return err
			}
		}

		var rows []spaceRecord
		if err := tx.
			Where("space = ? AND repo = ? AND collection = ? AND rkey = ?",
				uri, repo, collection, rkey).
			Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		if err := tx.
			Where("space = ? AND repo = ? AND collection = ? AND rkey = ?",
				uri, repo, collection, rkey).
			Delete(&spaceRecord{}).Error; err != nil {
			return err
		}

		// Fold the deleted records out of the cached LtHash.
		h, rev, _, err := loadRepoHash(tx, uri, repo)
		if err != nil {
			return err
		}
		for _, row := range rows {
			h.remove(recordElement(row.Collection.String(), row.Rkey.String(), row.Cid))
		}

		// Drop the hash row entirely once the repo holds no more records, so it
		// leaves the writer set reported by ListRepos.
		var remaining int64
		if err := tx.Model(&spaceRecord{}).
			Where("space = ? AND repo = ?", uri, repo).
			Count(&remaining).Error; err != nil {
			return err
		}
		if remaining == 0 {
			return tx.Where("space = ? AND repo = ?", uri, repo).Delete(&repoHashState{}).Error
		}
		return saveRepoHash(tx, uri, repo, h, rev)
	})
}
