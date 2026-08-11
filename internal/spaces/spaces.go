package spaces

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/openfga/pkg/tuple"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/events"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/spacecommit"
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
	PrevCid    string // cid of the record's prior version, for the oplog
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  gorm.DeletedAt
}

// spaceRepo caches a permissioned repo's LtHash so reads (listRepos,
// listRepoOps commit) don't rescan every record. State is the 2048-byte LtHash
// buffer, maintained incrementally in the write path (folded in on put, out on
// delete). Rev tracks the repo's latest write revision.
type spaceRepo struct {
	Space     habitat_syntax.SpaceURI `gorm:"primaryKey"`
	Repo      syntax.DID              `gorm:"primaryKey"`
	Hash      []byte
	Rev       syntax.TID
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt
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
	Prev       string
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
	// ListSpaces returns the URIs of the spaces `member` holds a permissioned
	// repo in — the spaces it has written at least one record to — most
	// recently written first. A space `member` owns but has never written to is
	// not listed, and neither is one whose records it has since deleted.
	ListSpaces(
		ctx context.Context,
		member syntax.DID,
		filterOwner *syntax.DID,
		filterType *syntax.NSID,
	) ([]habitat_syntax.SpaceURI, error)

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
	// CreateRecord writes a new record, failing with ErrRecordAlreadyExists if
	// one is already present at collection/rkey (an empty rkey always creates,
	// since one is minted fresh).
	CreateRecord(
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
	ListRecordBlocks(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		repo syntax.DID,
	) ([]recordBlock, error)
	// RepoSnapshot returns a repo's head rev/hash together with its record
	// blocks, read as of the same point: on Postgres both reads happen inside
	// the same advisory-locked transaction PutRecord/DeleteRecord use, so a
	// concurrent write cannot land between them. found is false when the repo
	// holds no records in the space.
	RepoSnapshot(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		repo syntax.DID,
	) (rev string, hash []byte, blocks []recordBlock, found bool, err error)
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

// Notifier is notified when a space changes so it can deliver events to
// registered syncers. Implementations must be non-blocking and best-effort.
type Notifier interface {
	// NotifyWrite reports that a repo advanced to a new revision within a
	// space, carrying the repo's LtHash commit state so syncers can detect
	// writes that arrive at the same rev but a different hash.
	NotifyWrite(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		repo syntax.DID,
		rev syntax.TID,
		hash []byte,
	)
	// NotifySpaceDeleted reports that a space was deleted.
	NotifySpaceDeleted(ctx context.Context, space habitat_syntax.SpaceURI)
}

var (
	ErrSpaceNotFound       = errors.New("space not found")
	ErrSpaceAlreadyExists  = errors.New("space already exists")
	ErrRecordNotFound      = errors.New("record not found")
	ErrUserAlreadyMember   = errors.New("user is already a member of the space")
	ErrNotAMember          = errors.New("user is not a member of the space")
	ErrCannotRemoveOrg     = errors.New("cannot remove the org from the space")
	ErrRepoNotFound        = errors.New("repo not found")
	ErrRevTooFar           = errors.New("since revision is ahead of the repo head")
	ErrRecordAlreadyExists = errors.New("record already exists")
)

// ---- Store implementation ----

type store struct {
	db         *gorm.DB
	fga        fgastore.Store
	clock      *syntax.TIDClock
	eventStore events.Store
	notifier   Notifier
}

var _ Store = &store{}

// NewStore creates a spaces store. notifier may be nil to disable notifyWrite
// delivery.
func NewStore(
	db *gorm.DB,
	fga fgastore.Store,
	eventStore events.Store,
	notifier Notifier,
) (*store, error) {
	if err := db.AutoMigrate(&space{}, &spaceRecord{}, &spaceRepo{}); err != nil {
		return nil, fmt.Errorf("failed to migrate spaces tables: %w", err)
	}
	return &store{
		db:         db,
		fga:        fga,
		clock:      syntax.NewTIDClock(0),
		eventStore: eventStore,
		notifier:   notifier,
	}, nil
}

// WithTx implements [Store], returning a store whose DB operations run on tx.
func (s *store) WithTx(tx *gorm.DB) Store {
	return &store{
		db:         tx,
		fga:        s.fga,
		clock:      s.clock,
		eventStore: s.eventStore,
		notifier:   s.notifier,
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
	member syntax.DID,
	filterOwner *syntax.DID,
	filterType *syntax.NSID,
) ([]habitat_syntax.SpaceURI, error) {
	// The writer set is the whole answer: spaceRepo holds one row per repo a
	// member has written into a space, keyed by the space URI, so the URIs are
	// read straight out of it. The spaces table is not consulted, which means a
	// space nobody has written to is not listed at all.
	query := s.db.WithContext(ctx).
		Model(&spaceRepo{}).
		Where("repo = ?", member)
	if filterOwner != nil || filterType != nil {
		query = query.Where(`space LIKE ? ESCAPE '\'`, spaceURIPattern(filterOwner, filterType))
	}

	var uris []habitat_syntax.SpaceURI
	if err := query.Order("updated_at DESC").Pluck("space", &uris).Error; err != nil {
		return nil, fmt.Errorf("list written spaces: %w", err)
	}
	return uris, nil
}

// spaceURIPattern builds a LIKE pattern matching the stored space URIs with the
// given owner and type; a nil filter matches any value. Stored URIs are always
// in the current format ("at://<did>/space/<type>/<skey>"), whose literal
// separators anchor each component — and since neither a DID nor an NSID can
// contain "/", a wildcard cannot spill into the neighbouring component.
func spaceURIPattern(owner *syntax.DID, spaceType *syntax.NSID) string {
	ownerPattern := "%"
	if owner != nil {
		ownerPattern = escapeLike(owner.String())
	}
	typePattern := "%"
	if spaceType != nil {
		typePattern = escapeLike(spaceType.String())
	}
	return fmt.Sprintf("at://%s/space/%s/%%", ownerPattern, typePattern)
}

// likeEscaper escapes the LIKE wildcards in a value interpolated into a
// pattern, for use with "ESCAPE '\'". A DID can legitimately carry both: a
// did:web holding a port percent-encodes it (did:web:example.com%3A8080), and %
// would otherwise match anything.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func escapeLike(s string) string {
	return likeEscaper.Replace(s)
}

// loadRepoHash reads a repo's cached LtHash state and rev, or the zero hash and
// empty rev (found=false) when no row exists yet.
func loadRepoHash(
	tx *gorm.DB,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
) (spacecommit.LtHash, syntax.TID, bool, error) {
	var row spaceRepo
	err := tx.Unscoped().Where("space = ? AND repo = ?", space, repo).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || row.DeletedAt.Valid {
		return spacecommit.LtHash{}, "", false, nil
	}
	if err != nil {
		return spacecommit.LtHash{}, "", false, err
	}
	return spacecommit.Load(row.Hash), row.Rev, true, nil
}

// saveRepoHash persists a repo's LtHash state and rev.
func saveRepoHash(
	tx *gorm.DB,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	h spacecommit.LtHash,
	rev syntax.TID,
) error {
	return tx.Save(&spaceRepo{
		Space: space,
		Repo:  repo,
		Hash:  h.State(),
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
	var rows []spaceRepo
	if err := s.db.WithContext(ctx).
		Where("space = ?", uri).
		Order("repo ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	repos := make([]RepoInfo, len(rows))
	for i, row := range rows {
		h := spacecommit.Load(row.Hash)
		repos[i] = RepoInfo{
			DID:  row.Repo,
			Rev:  string(row.Rev),
			Hash: h.Sum(),
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
	return s.writeRecord(ctx, spaceUri, repo, collection, rkey, value, false)
}

// CreateRecord writes a new record, per network.habitat.space.createRecord:
// unlike PutRecord, it fails with ErrRecordAlreadyExists rather than
// overwriting one already present at collection/rkey.
func (s *store) CreateRecord(
	ctx context.Context,
	spaceUri habitat_syntax.SpaceURI,
	repo syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
	value map[string]any,
) (habitat_syntax.SpaceRecordURI, *cid.Cid, error) {
	return s.writeRecord(ctx, spaceUri, repo, collection, rkey, value, true)
}

func (s *store) writeRecord(
	ctx context.Context,
	spaceUri habitat_syntax.SpaceURI,
	repo syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
	value map[string]any,
	requireNew bool,
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
		return "", nil, fmt.Errorf("failed to get space: %w", err)
	}

	bytes, err := atdata.MarshalCBOR(value)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal record: %w", err)
	}

	cid, err := cid.NewPrefixV1(cid.DagCBOR, multihash.SHA2_256).Sum(bytes)
	if err != nil {
		return "", nil, fmt.Errorf("failed to compute cid: %w", err)
	}

	var recordUri habitat_syntax.SpaceRecordURI
	var newRev syntax.TID
	var repoHash []byte
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if tx.Name() == "postgres" {
			// acquire lock on permissioned repo within space
			if err := tx.Exec(
				`SELECT pg_advisory_xact_lock(hashtext(?), hashtext(?))`,
				spaceUri,
				repo,
			).Error; err != nil {
				return fmt.Errorf("failed to acquire lock: %w", err)
			}
		}
		if requireNew && rkey != "" {
			var count int64
			if err := tx.Model(&spaceRecord{}).
				Where("space = ? AND repo = ? AND collection = ? AND rkey = ?",
					spaceUri, repo, collection, rkey).
				Count(&count).Error; err != nil {
				return fmt.Errorf("failed to check existing record: %w", err)
			}
			if count > 0 {
				return ErrRecordAlreadyExists
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
			return fmt.Errorf("failed to get previous record: %w", err)
		}
		tid := s.clock.Next()
		newRev = tid
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
			return fmt.Errorf("failed to append event: %w", err)
		}

		h, _, _, err := loadRepoHash(tx, spaceUri, repo)
		if err != nil {
			return fmt.Errorf("failed to load repo hash: %w", err)
		}
		// Maintain the cached LtHash: fold out this record's previous element (if
		// it already existed) and fold in the new one, then advance the rev.
		var existing spaceRecord
		err = tx.
			Where("space = ? AND repo = ? AND collection = ? AND rkey = ?",
				spaceUri, repo, collection, rkey).
			First(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to get existing record: %w", err)
		} else if err == nil {
			h.Remove(spacecommit.RecordElement(collection, rkey, existing.Cid))
		}
		h.Add(spacecommit.RecordElement(collection, rkey, cid.String()))
		if err := saveRepoHash(tx, spaceUri, repo, h, tid); err != nil {
			return fmt.Errorf("failed to save repo hash: %w", err)
		}
		repoHash = h.Sum()
		prevCid := ""
		if err == nil {
			prevCid = existing.Cid
		}

		return tx.Save(&spaceRecord{
			Repo:       repo,
			Space:      spaceUri,
			Collection: collection,
			Rkey:       rkey,
			Value:      bytes,
			Rev:        tid,
			PrevCid:    prevCid,
			Cid:        cid.String(),
		}).Error
	})
	if err != nil {
		return "", nil, fmt.Errorf("failed to create record: %w", err)
	}
	s.eventStore.NotifyEvent(ctx)
	// Best-effort: notify registered syncers that this repo advanced.
	s.notifier.NotifyWrite(ctx, spaceUri, repo, newRev, repoHash)
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

func (s *store) ListRecordBlocks(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	repo syntax.DID,
) ([]recordBlock, error) {
	var sp space
	err := s.db.WithContext(ctx).
		Where("owner = ? AND skey = ?", uri.SpaceOwner(), uri.Skey()).
		First(&sp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSpaceNotFound
	} else if err != nil {
		return nil, err
	}

	var rows []spaceRecord
	if err := s.db.WithContext(ctx).
		Where("space = ?", uri).
		Where("repo = ?", repo).
		Order("collection ASC, rkey ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	blocks := make([]recordBlock, len(rows))
	for i, row := range rows {
		blocks[i] = recordBlock{
			Collection: row.Collection,
			Rkey:       row.Rkey,
			Cid:        cid.MustParse(row.Cid),
			Bytes:      row.Value,
		}
	}
	return blocks, nil
}

func (s *store) RepoSnapshot(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	repo syntax.DID,
) (revision string, hash []byte, blocks []recordBlock, found bool, err error) {
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if tx.Name() == "postgres" {
			// The same per-repo lock PutRecord/DeleteRecord hold for their
			// whole write blocks until this read finishes, and blocks any new
			// writer from starting until it does: the head and the blocks
			// below are read as of the same point, with nothing landing
			// between them.
			if err := tx.Exec(
				`SELECT pg_advisory_xact_lock(hashtext(?), hashtext(?))`, uri, repo,
			).Error; err != nil {
				return fmt.Errorf("failed to acquire lock: %w", err)
			}
		}

		var sp space
		err := tx.
			Where("owner = ? AND skey = ?", uri.SpaceOwner(), uri.Skey()).
			First(&sp).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSpaceNotFound
		} else if err != nil {
			return err
		}

		h, rev, ok, err := loadRepoHash(tx, uri, repo)
		if err != nil {
			return fmt.Errorf("repo head: %w", err)
		}
		if !ok {
			return nil
		}
		revision, hash, found = rev.String(), h.Sum(), true

		var rows []spaceRecord
		if err := tx.
			Where("space = ?", uri).
			Where("repo = ?", repo).
			Order("collection ASC, rkey ASC").
			Find(&rows).Error; err != nil {
			return err
		}
		blocks = make([]recordBlock, len(rows))
		for i, row := range rows {
			blocks[i] = recordBlock{
				Collection: row.Collection,
				Rkey:       row.Rkey,
				Cid:        cid.MustParse(row.Cid),
				Bytes:      row.Value,
			}
		}
		return nil
	})
	if err != nil {
		return "", nil, nil, false, err
	}
	return revision, hash, blocks, found, nil
}

func (s *store) DeleteSpace(ctx context.Context, uri habitat_syntax.SpaceURI) error {
	// read the stored FGA tuples for this space before deleting anything,
	// so we know exactly what tuples to delete
	tuples, err := s.fga.Read(ctx, fgastore.Tuple{Object: fgastore.SpaceObjectKey(uri)})
	if err != nil {
		return err
	}

	// everything after this point is idempotent — use a transaction
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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

		// Drop the permissioned repos along with the records they cached a
		// hash of. They are the writer set listSpaces reads, so leaving them
		// behind would keep a deleted space on its writers' listings.
		if err := tx.
			Where("space = ?", uri).
			Delete(&spaceRepo{}).Error; err != nil {
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
		return err
	}
	// Best-effort: tell registered syncers the space is gone.
	s.notifier.NotifySpaceDeleted(ctx, uri)
	return nil
}

func (s *store) ListRepoOps(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	repo syntax.DID,
	since string,
	limit int,
) ([]Record, error) {
	// A since beyond the repo's head revision is a client error: nothing will
	// ever be listed after it, and an empty page would be indistinguishable
	// from the normal at-head case. Syncers use the RevNotFound error to detect
	// they are ahead of the host and resync from scratch.
	if since != "" {
		var head spaceRepo
		err := s.db.WithContext(ctx).
			Where("space = ? AND repo = ?", uri, repo).
			First(&head).Error
		if err == nil && since > string(head.Rev) {
			return nil, ErrRevTooFar
		}
	}
	query := s.db.WithContext(ctx).
		Unscoped().
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
		if row.DeletedAt.Valid {
			records[i] = Record{
				Owner:      row.Repo,
				Collection: row.Collection,
				Rkey:       row.Rkey,
				Value:      nil,
				Rev:        string(row.Rev),
				Prev:       row.PrevCid,
				UpdatedAt:  row.DeletedAt.Time,
				// empty cid
			}
		} else {
			records[i] = Record{
				Owner:      row.Repo,
				Collection: row.Collection,
				Rkey:       row.Rkey,
				Value:      value,
				Rev:        string(row.Rev),
				Prev:       row.PrevCid,
				UpdatedAt:  row.UpdatedAt,
				Cid:        cid.MustParse(row.Cid),
			}
		}
	}
	return records, nil
}

func (s *store) RepoHead(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	repo syntax.DID,
) (revision string, sum []byte, found bool, err error) {
	h, rev, found, err := loadRepoHash(s.db.WithContext(ctx), uri, repo)
	if err != nil {
		return "", nil, false, fmt.Errorf("repo head: %w", err)
	}
	if !found {
		return "", nil, false, nil
	}
	return rev.String(), h.Sum(), true, nil
}

func (s *store) DeleteRecord(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	repo syntax.DID,
	collection syntax.NSID,
	rkey string,
) error {
	var outRev syntax.TID
	var outHash []byte
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		rev := s.clock.Next()
		if err := tx.Model(&spaceRecord{}).
			Where("space = ? AND repo = ? AND collection = ? AND rkey = ?",
				uri, repo, collection, rkey).
			Updates(map[string]any{
				"deleted_at": time.Now(),
				"rev":        rev,
				"prev_cid":   rows[0].Cid,
			}).Error; err != nil {
			return fmt.Errorf("delete record: %w", err)
		}
		// Propagate the delete to syncers as a space event carrying no value.
		for _, row := range rows {
			recordUri := habitat_syntax.ConstructSpaceRecordURI(uri, repo, row.Collection, row.Rkey)
			if err := s.eventStore.WithTx(tx).AppendSpaceEvent(
				ctx,
				uri,
				repo,
				rev,
				row.Rev,
				[]events.EventOps{
					{
						Action: "delete",
						Uri:    recordUri,
						Value:  nil,
						Cid:    "",
					},
				},
			); err != nil {
				return fmt.Errorf("append delete event: %w", err)
			}
		}
		// Fold the deleted records out of the cached LtHash.
		h, _, _, err := loadRepoHash(tx, uri, repo)
		if err != nil {
			return err
		}
		for _, row := range rows {
			h.Remove(spacecommit.RecordElement(row.Collection, row.Rkey, row.Cid))
		}
		outRev = rev
		outHash = h.Sum()
		// Drop the hash row entirely once the repo holds no more records
		var remaining int64
		if err := tx.Model(&spaceRecord{}).
			Where("space = ? AND repo = ?", uri, repo).
			Count(&remaining).Error; err != nil {
			return err
		}
		if remaining == 0 {
			return tx.Where("space = ? AND repo = ?", uri, repo).Delete(&spaceRepo{}).Error
		}
		return saveRepoHash(tx, uri, repo, h, rev)
	})
	if err != nil {
		return fmt.Errorf("delete record: %w", err)
	}
	if outRev == "" {
		return nil
	}
	s.eventStore.NotifyEvent(ctx)
	// Best-effort: tell syncers the repo advanced so they pull the delete op.
	s.notifier.NotifyWrite(ctx, uri, repo, outRev, outHash)
	return nil
}
