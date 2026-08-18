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
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/spacecommit"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// GORM models

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

// Store defines the persistence interface for spaces
// Implements methods defined in proposal here:
// https://github.com/bluesky-social/proposals/tree/main/0016-permissioned-data#xrpc-api
type Store interface {
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
	// RepoSnapshot returns a repo's signed head commit together with its record
	// blocks, read as of the same point: on Postgres both reads happen inside
	// the same advisory-locked transaction PutRecord/DeleteRecord use, so a
	// concurrent write cannot land between them, and the commit is built from
	// that same frozen rev/hash. commit is nil when the repo holds no records
	// in the space.
	RepoSnapshot(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		repo syntax.DID,
	) (commit *spacecommit.SignedCommit, blocks []recordBlock, err error)
	DeleteRecord(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		repo syntax.DID,
		collection syntax.NSID,
		rkey string,
	) error
	DeleteSpaceRepos(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
	) error

	// Oplog operations
	//
	// ListRepoOps returns a repo's operations within a space after a given
	// revision, ordered by revision ascending, for incremental sync. When this
	// page reaches the head of the oplog, commit is the signed commit over
	// exactly the state these ops leave the caller at — the head is read inside
	// the same locked transaction as the ops, so it can never describe a write
	// that landed between the two reads. commit is nil when the page does not
	// reach the head, or the repo has no records to sign a commit over.
	ListRepoOps(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		repo syntax.DID,
		since string,
		limit int,
	) (ops []Record, commit *spacecommit.SignedCommit, err error)

	// RepoHead returns a repo's current head revision and LtHash commit hash.
	// found is false when the repo holds no records in the space.
	RepoHead(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		repo syntax.DID,
	) (rev string, hash []byte, found bool, err error)

	// RepoHeadCommit returns the signed commit over a repo's current head
	// state. commit is nil when the repo holds no records in the space.
	RepoHeadCommit(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		repo syntax.DID,
	) (commit *spacecommit.SignedCommit, err error)

	// WithTx returns a copy of the store scoped to the given transaction, so its
	// DB writes participate in a caller-managed transaction. FGA writes are not
	// transactional with the DB, but callers run them inside the same closure so
	// a DB rollback follows an FGA failure.
	db.Store[Store]
}

// Notifier is notified when a space changes so it can deliver events to
// registered syncers. Implementations must be non-blocking and best-effort.
type Notifier interface {
	// NotifyWrite reports that a repo advanced to a new revision within a space.
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
	ErrRecordNotFound    = errors.New("record not found")
	ErrUserAlreadyMember = errors.New("user is already a member of the space")
	ErrNotAMember        = errors.New("user is not a member of the space")
	ErrRepoNotFound      = errors.New("repo not found")
	ErrRevTooFar         = errors.New("since revision is ahead of the repo head")
)

// ---- Store implementation ----

type store struct {
	db       *gorm.DB
	fga      fgastore.Store
	clock    *syntax.TIDClock
	notifier Notifier
	commit   *spacecommit.Authority
}

// DeleteSpaceRepos implements [Store].
func (s *store) DeleteSpaceRepos(ctx context.Context, space habitat_syntax.SpaceURI) error {

	if err := s.db.
		Where("space = ?", space).
		Delete(&spaceRecord{}).Error; err != nil {
		return err
	}

	if err := s.db.
		Where("space = ?", space).
		Delete(&spaceRecord{}).Error; err != nil {
		return err
	}

	return nil
}

var _ Store = &store{}

// NewStore creates a spaces store. notifier may be nil to disable notifyWrite
// delivery. commit signs the repo-head commits RepoSnapshot, ListRepoOps, and
// RepoHeadCommit build.
func NewStore(
	db *gorm.DB,
	fga fgastore.Store,
	notifier Notifier,
	commit *spacecommit.Authority,
) (*store, error) {
	if err := db.AutoMigrate(&spaceRecord{}, &spaceRepo{}); err != nil {
		return nil, fmt.Errorf("failed to migrate spaces tables: %w", err)
	}
	return &store{
		db:       db,
		fga:      fga,
		clock:    syntax.NewTIDClock(0),
		notifier: notifier,
		commit:   commit,
	}, nil
}

// WithTx implements [Store], returning a store whose DB operations run on tx.
func (s *store) WithTx(tx *gorm.DB) Store {
	return &store{
		db:       tx,
		fga:      s.fga,
		clock:    s.clock,
		notifier: s.notifier,
		commit:   s.commit,
	}
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

// lockRepo acquires the per-repo advisory lock PutRecord/DeleteRecord hold for
// their whole write, so a read inside the same transaction cannot race a
// concurrent writer: it blocks until an in-flight write finishes and holds the
// lock until the read commits. It is a no-op outside Postgres, where SQLite
// serializes whole transactions already.
func lockRepo(tx *gorm.DB, space habitat_syntax.SpaceURI, repo syntax.DID) error {
	if tx.Name() != "postgres" {
		return nil
	}
	if err := tx.Exec(
		`SELECT pg_advisory_xact_lock(hashtext(?), hashtext(?))`, space, repo,
	).Error; err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	return nil
}

func (s *store) ListRepos(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
) ([]RepoInfo, error) {
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
		fgastore.OwnerContextualTuple(uri),
		fgastore.OrgMemberContextualTuple(org),
	)
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
		if err := lockRepo(tx, spaceUri, repo); err != nil {
			return err
		}
		tid := s.clock.Next()
		newRev = tid
		if rkey == "" {
			rkey = syntax.RecordKey(tid)
		}
		recordUri = habitat_syntax.ConstructSpaceRecordURI(spaceUri, repo, collection, rkey)

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

func (s *store) RepoSnapshot(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	repo syntax.DID,
) (commit *spacecommit.SignedCommit, blocks []recordBlock, err error) {
	var revision string
	var hash []byte
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockRepo(tx, uri, repo); err != nil {
			return err
		}

		h, rev, ok, err := loadRepoHash(tx, uri, repo)
		if err != nil {
			return fmt.Errorf("repo head: %w", err)
		}
		if !ok {
			return nil
		}
		revision, hash = rev.String(), h.Sum()

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
		return nil, nil, err
	}
	if revision == "" {
		return nil, nil, nil
	}
	signed, err := s.commit.Build(ctx, uri, repo, revision, hash)
	if err != nil {
		return nil, nil, fmt.Errorf("build commit: %w", err)
	}
	return &signed, blocks, nil
}

func (s *store) ListRepoOps(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	repo syntax.DID,
	since string,
	limit int,
) (records []Record, commit *spacecommit.SignedCommit, err error) {
	if limit <= 0 {
		limit = 100
	}
	var headRev syntax.TID
	var headHash []byte
	var headFound bool
	var rows []spaceRecord
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockRepo(tx, uri, repo); err != nil {
			return err
		}

		h, rev, found, err := loadRepoHash(tx, uri, repo)
		if err != nil {
			return fmt.Errorf("repo head: %w", err)
		}
		headRev, headHash, headFound = rev, h.Sum(), found
		if since != "" && found && since > string(rev) {
			return ErrRevTooFar
		}

		query := tx.Unscoped().Model(&spaceRecord{}).Where("space = ? AND repo = ?", uri, repo)
		if since != "" {
			query = query.Where("rev > ?", since)
		}
		return query.Order("rev ASC").Limit(limit).Find(&rows).Error
	})
	if err != nil {
		return nil, nil, fmt.Errorf("list repo ops: %w", err)
	}

	records = make([]Record, len(rows))
	for i, row := range rows {
		value, err := atdata.UnmarshalCBOR(row.Value)
		if err != nil {
			return nil, nil, err
		}
		if row.DeletedAt.Valid {
			records[i] = Record{
				Owner:      row.Repo,
				Collection: row.Collection,
				Rkey:       row.Rkey,
				Rev:        string(row.Rev),
				Prev:       row.PrevCid,
				UpdatedAt:  row.DeletedAt.Time,
				// empty cid and value
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
	if len(rows) < limit && headFound {
		signed, err := s.commit.Build(ctx, uri, repo, headRev.String(), headHash)
		if err != nil {
			return nil, nil, fmt.Errorf("build commit: %w", err)
		}
		commit = &signed
	}
	return records, commit, nil
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

// RepoHeadCommit implements [Store].
func (s *store) RepoHeadCommit(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	repo syntax.DID,
) (commit *spacecommit.SignedCommit, err error) {
	rev, hash, found, err := s.RepoHead(ctx, uri, repo)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	signed, err := s.commit.Build(ctx, uri, repo, rev, hash)
	if err != nil {
		return nil, fmt.Errorf("build commit: %w", err)
	}
	return &signed, nil
}

func (s *store) DeleteRecord(
	ctx context.Context,
	uri habitat_syntax.SpaceURI,
	repo syntax.DID,
	collection syntax.NSID,
	rkey string,
) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockRepo(tx, uri, repo); err != nil {
			return err
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
		// Fold the deleted records out of the cached LtHash.
		h, _, _, err := loadRepoHash(tx, uri, repo)
		if err != nil {
			return err
		}
		for _, row := range rows {
			h.Remove(spacecommit.RecordElement(row.Collection, row.Rkey, row.Cid))
		}
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
}
