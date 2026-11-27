package spaces

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/habitat-network/habitat/internal/spacecommit"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/telemetry"
)

// ErrRepoNotRemote is returned for a repo that isn't a Remote writer of the
// space: its records are stored here, or it isn't a writer at all.
var ErrRepoNotRemote = errors.New("repo is not a remote writer of the space")

// spaceRemoteRecord is a copy of a record an external repo holds on its own PDS,
// fetched from there by the space host so the space's event log
// (network.habitat.space.subscribe) carries it. It is kept apart from
// spaceRecord, which only holds records written through this host, so none of
// the repo-host reads (getRecord, listRepoOps, the repo's LtHash) serve it.
//
// Like spaceRecord, a row holds the record's latest version and is
// soft-deleted by a delete, so the log keeps the delete.
type spaceRemoteRecord struct {
	Space habitat_syntax.SpaceURI
	// SpaceRef holds the parts of Space and is the key. Space is the legacy
	// URI column, still written until it is dropped.
	SpaceRef   habitat_syntax.SpaceRef `gorm:"embedded;embeddedPrefix:space_"`
	Repo       syntax.DID              `gorm:"primaryKey"`
	Collection syntax.NSID             `gorm:"primaryKey"`
	Rkey       syntax.RecordKey        `gorm:"primaryKey"`
	// Value is the record's DAG-CBOR encoding.
	Value []byte
	Cid   string
	// PrevCid is the CID of the version this one replaced, empty for a
	// create.
	PrevCid string
	Rev     syntax.TID
	// Seq is the record's latest write's position in the event log: 0,
	// pending, until [Store.SequenceEvents] numbers it.
	Seq       EventSeq `gorm:"not null;default:0;index"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt
}

// spaceRemoteSync is how far the space host has fetched an external repo:
// the rev of the last op fetched, and the LtHash folded from every op so far,
// which is checked against the repo's commit.
type spaceRemoteSync struct {
	Space habitat_syntax.SpaceURI
	// SpaceRef holds the parts of Space and is the key. Space is the legacy
	// URI column, still written until it is dropped.
	SpaceRef  habitat_syntax.SpaceRef `gorm:"embedded;embeddedPrefix:space_"`
	Repo      syntax.DID              `gorm:"primaryKey"`
	Rev       syntax.TID
	State     []byte
	UpdatedAt time.Time
}

// RemoteRepo is an external repo of a space, as far as the space host has
// fetched it, and as far as its PDS has said it has advanced.
type RemoteRepo struct {
	Space habitat_syntax.SpaceURI
	Repo  syntax.DID
	// SyncedRev is the rev of the last op fetched; empty before the first
	// fetch.
	SyncedRev syntax.TID
	// State is the LtHash folded from every op fetched.
	State spacecommit.LtHash
	// ReportedRev is the rev the repo's latest notifyWrite reported.
	ReportedRev syntax.TID
}

// Behind reports whether the repo has advanced past what was fetched.
func (r RemoteRepo) Behind() bool {
	return r.SyncedRev < r.ReportedRev
}

// RemoteOp is one op fetched from an external repo's listRepoOps.
type RemoteOp struct {
	Rev        syntax.TID
	Collection syntax.NSID
	Rkey       syntax.RecordKey
	// Cid is empty for a delete.
	Cid string
	// Prev is the CID of the version the op replaced, empty for a create.
	Prev string
	// Value is the record's DAG-CBOR encoding, nil for a delete.
	Value []byte
}

// RemoteSync is a batch of fetched ops to store, and the fetch position
// after it.
type RemoteSync struct {
	Ops []RemoteOp
	// Rev and State are the repo's fetch position after Ops.
	Rev   syntax.TID
	State spacecommit.LtHash
	// Rebuild marks a fetch from the start of the repo's oplog: every record
	// the repo still holds is in Ops, so a stored record that isn't was
	// deleted while it wasn't being followed, and is deleted here too.
	Rebuild bool
}

// GetRemoteRepo implements [Store].
func (s *store) GetRemoteRepo(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
) (RemoteRepo, error) {
	var row spaceRepo
	err := s.db.WithContext(ctx).
		Scopes(habitat_syntax.InSpace(space)).Where("repo = ? AND remote = ?", repo, true).
		Where("removed IS NULL OR removed = ?", false).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return RemoteRepo{}, ErrRepoNotRemote
	} else if err != nil {
		return RemoteRepo{}, fmt.Errorf("load repo: %w", err)
	}
	remote := RemoteRepo{Space: space, Repo: repo, ReportedRev: row.Rev}
	var sync spaceRemoteSync
	err = s.db.WithContext(ctx).
		Scopes(habitat_syntax.InSpace(space)).Where("repo = ?", repo).
		Limit(1).Find(&sync).Error
	if err != nil {
		return RemoteRepo{}, fmt.Errorf("load remote sync: %w", err)
	}
	remote.SyncedRev = sync.Rev
	remote.State = spacecommit.Load(sync.State)
	return remote, nil
}

// ListRemoteReposBehind implements [Store].
func (s *store) ListRemoteReposBehind(ctx context.Context) ([]RemoteRepo, error) {
	var rows []struct {
		habitat_syntax.SpaceRef `gorm:"embedded;embeddedPrefix:space_"`
		Repo                    syntax.DID
		ReportedRev             syntax.TID
		SyncedRev               *syntax.TID
		State                   []byte
	}
	err := s.db.WithContext(ctx).Table("space_repos AS r").
		Select("r.space_owner, r.space_type, r.space_skey, r.repo, r.rev AS reported_rev, s.rev AS synced_rev, s.state").
		Joins("LEFT JOIN space_remote_syncs AS s ON s.space_owner = r.space_owner AND s.space_type = r.space_type AND s.space_skey = r.space_skey AND s.repo = r.repo").
		Where("r.remote = ? AND r.deleted_at IS NULL", true).
		Where("r.removed IS NULL OR r.removed = ?", false).
		Where("s.rev IS NULL OR s.rev < r.rev").
		Order("r.space_owner, r.space_type, r.space_skey, r.repo").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list remote repos: %w", err)
	}
	repos := make([]RemoteRepo, len(rows))
	for i, row := range rows {
		repos[i] = RemoteRepo{
			Space:       row.URI(),
			Repo:        row.Repo,
			ReportedRev: row.ReportedRev,
			State:       spacecommit.Load(row.State),
		}
		if row.SyncedRev != nil {
			repos[i].SyncedRev = *row.SyncedRev
		}
	}
	return repos, nil
}

// ApplyRemoteSync implements [Store].
func (s *store) ApplyRemoteSync(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	sync RemoteSync,
) (err error) {
	ctx, span := tracer.Start(ctx, "spaces.ApplyRemoteSync", trace.WithAttributes(
		telemetry.Space(space),
		telemetry.Repo(repo),
		attribute.Int("habitat.ops", len(sync.Ops)),
		attribute.Bool("habitat.rebuild", sync.Rebuild),
	))
	defer func() { telemetry.End(span, err) }()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Ops come in rev order, so a record's last op is its current state.
		touched := map[[2]string]bool{}
		for _, op := range sync.Ops {
			touched[[2]string{op.Collection.String(), op.Rkey.String()}] = true
			if err := applyRemoteOp(tx, space, repo, op); err != nil {
				return err
			}
		}
		if sync.Rebuild {
			if err := deleteUntouched(tx, space, repo, sync.Rev, touched); err != nil {
				return err
			}
		}
		return tx.Save(&spaceRemoteSync{
			Space:    space,
			SpaceRef: space.Ref(),
			Repo:     repo,
			Rev:      sync.Rev,
			State:    sync.State.State(),
		}).Error
	})
	if err != nil {
		return fmt.Errorf("apply remote sync: %w", err)
	}
	return nil
}

// applyRemoteOp stores one fetched op as its record's latest write, pending a
// seq.
func applyRemoteOp(
	tx *gorm.DB,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	op RemoteOp,
) error {
	key := "repo = ? AND collection = ? AND rkey = ?"
	if op.Cid == "" {
		if err := tx.Unscoped().Model(&spaceRemoteRecord{}).
			Scopes(habitat_syntax.InSpace(space)).Where(key, repo, op.Collection, op.Rkey).
			Updates(map[string]any{
				"deleted_at": time.Now(),
				"rev":        op.Rev,
				"prev_cid":   op.Prev,
				"seq":        0,
			}).Error; err != nil {
			return fmt.Errorf("delete remote record: %w", err)
		}
		return nil
	}
	now := time.Now()
	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "space_owner"},
			{Name: "space_type"},
			{Name: "space_skey"},
			{Name: "repo"},
			{Name: "collection"},
			{Name: "rkey"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"value", "cid", "prev_cid", "rev", "seq", "updated_at", "deleted_at",
		}),
	}).Create(&spaceRemoteRecord{
		Space:      space,
		SpaceRef:   space.Ref(),
		Repo:       repo,
		Collection: op.Collection,
		Rkey:       op.Rkey,
		Value:      op.Value,
		Cid:        op.Cid,
		PrevCid:    op.Prev,
		Rev:        op.Rev,
		CreatedAt:  now,
		UpdatedAt:  now,
	}).Error; err != nil {
		return fmt.Errorf("put remote record: %w", err)
	}
	return nil
}

// deleteUntouched deletes the stored records of repo that a rebuild's ops
// didn't name: the repo no longer holds them. The deletes are dated at rev,
// the rebuild's position, since when they happened isn't known.
func deleteUntouched(
	tx *gorm.DB,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	rev syntax.TID,
	touched map[[2]string]bool,
) error {
	var live []spaceRemoteRecord
	if err := tx.Select("collection", "rkey").
		Scopes(habitat_syntax.InSpace(space)).Where("repo = ?", repo).
		Find(&live).Error; err != nil {
		return fmt.Errorf("list remote records: %w", err)
	}
	for _, r := range live {
		if touched[[2]string{r.Collection.String(), r.Rkey.String()}] {
			continue
		}
		if err := tx.Unscoped().Model(&spaceRemoteRecord{}).
			Scopes(habitat_syntax.InSpace(space)).
			Where("repo = ? AND collection = ? AND rkey = ?", repo, r.Collection, r.Rkey).
			Updates(map[string]any{"deleted_at": time.Now(), "rev": rev, "seq": 0}).
			Error; err != nil {
			return fmt.Errorf("delete remote record: %w", err)
		}
	}
	return nil
}

// remoteCommitEvent is a fetched record's latest write as an event.
func remoteCommitEvent(row spaceRemoteRecord) (CommitEvent, error) {
	return commitEvent(spaceRecord{
		Space:      row.Space,
		Repo:       row.Repo,
		Collection: row.Collection,
		Rkey:       row.Rkey,
		Value:      row.Value,
		Rev:        row.Rev,
		Cid:        row.Cid,
		PrevCid:    row.PrevCid,
		Seq:        row.Seq,
		UpdatedAt:  row.UpdatedAt,
		DeletedAt:  row.DeletedAt,
	})
}

// BeforeSave stores the parts of Space in the space_* columns.
func (r *spaceRemoteRecord) BeforeSave(*gorm.DB) error {
	r.SpaceRef = r.Space.Ref()
	return nil
}

// AfterFind reads Space from the space_* columns.
func (r *spaceRemoteRecord) AfterFind(*gorm.DB) error {
	if r.SpaceRef != (habitat_syntax.SpaceRef{}) {
		r.Space = r.SpaceRef.URI()
	}
	return nil
}

// BeforeSave stores the parts of Space in the space_* columns.
func (r *spaceRemoteSync) BeforeSave(*gorm.DB) error {
	r.SpaceRef = r.Space.Ref()
	return nil
}

// AfterFind reads Space from the space_* columns.
func (r *spaceRemoteSync) AfterFind(*gorm.DB) error {
	if r.SpaceRef != (habitat_syntax.SpaceRef{}) {
		r.Space = r.SpaceRef.URI()
	}
	return nil
}
