package search

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/habitat-network/habitat/internal/db"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// searchDocument is the row backing a [Document]. Its table is created by the
// Atlas-generated schema migrations; the full-text structures on top of it (an
// FTS5 table on SQLite, a GIN index on Postgres) are created by a hand-written
// migration in cmd/pear/migrations, because Atlas can't model them.
type searchDocument struct {
	URI        string `gorm:"primaryKey"`
	Space      string `gorm:"index"`
	Repo       string
	Collection string
	Rev        string
	Body       string
}

// Models returns the GORM models this package persists. Their tables are
// created by pear's schema migrations in cmd/pear/migrations, which Atlas
// generates from these models.
func Models() []any {
	return []any{&searchDocument{}}
}

// New returns an Index backed by db's full-text search: FTS5 on SQLite,
// tsvector on Postgres. pear's migrations must already be applied, and SQLite
// needs a build with the sqlite_fts5 tag.
func New(gdb *gorm.DB) (Index, error) {
	switch db.DialectOf(gdb) {
	case db.Sqlite:
		return &sqliteIndex{db: gdb}, nil
	case db.Postgres:
		return &postgresIndex{db: gdb}, nil
	default:
		return nil, fmt.Errorf("unsupported database: %s", gdb.Name())
	}
}

// putDocuments upserts docs, keeping whichever revision of a record is newer.
// Both dialects share it; the full-text structures follow from the row writes.
func putDocuments(ctx context.Context, gdb *gorm.DB, docs []Document) error {
	if len(docs) == 0 {
		return nil
	}
	// One statement can't upsert the same URI twice, so keep the newest
	// revision of each.
	newest := make(map[string]searchDocument, len(docs))
	for _, d := range docs {
		row := searchDocument{
			URI:        d.URI.String(),
			Space:      d.Space.String(),
			Repo:       d.Repo.String(),
			Collection: d.Collection.String(),
			Rev:        d.Rev.String(),
			Body:       d.Text,
		}
		if prev, ok := newest[row.URI]; !ok || prev.Rev < row.Rev {
			newest[row.URI] = row
		}
	}
	rows := slices.Collect(maps.Values(newest))
	return gdb.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "uri"}},
		UpdateAll: true,
		Where: clause.Where{Exprs: []clause.Expression{
			clause.Expr{SQL: "search_documents.rev < excluded.rev"},
		}},
	}).CreateInBatches(rows, 100).Error
}

func deleteDocuments(
	ctx context.Context,
	gdb *gorm.DB,
	uris []habitat_syntax.SpaceRecordURI,
) error {
	if len(uris) == 0 {
		return nil
	}
	raw := make([]string, len(uris))
	for i, u := range uris {
		raw[i] = u.String()
	}
	return gdb.WithContext(ctx).Where("uri IN ?", raw).Delete(&searchDocument{}).Error
}

func deleteSpaceDocuments(
	ctx context.Context,
	gdb *gorm.DB,
	space habitat_syntax.SpaceURI,
) error {
	return gdb.WithContext(ctx).Where("space = ?", space.String()).Delete(&searchDocument{}).Error
}

// pageParams resolves q's limit and decodes its offset cursor.
func pageParams(q Query) (limit, offset int, err error) {
	limit = q.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if q.Cursor != "" {
		offset, err = strconv.Atoi(q.Cursor)
		if err != nil || offset < 0 {
			return 0, 0, fmt.Errorf("invalid cursor %q", q.Cursor)
		}
	}
	return limit, offset, nil
}

// nextCursor is the cursor for the page after one that returned n hits at
// offset, or "" when the page wasn't full.
func nextCursor(n, limit, offset int) string {
	if n < limit {
		return ""
	}
	return strconv.Itoa(offset + limit)
}

func toResult(rows []searchRow, limit, offset int) Result {
	hits := make([]Hit, len(rows))
	for i, r := range rows {
		hits[i] = Hit{
			URI:        habitat_syntax.SpaceRecordURI(r.URI),
			Space:      habitat_syntax.SpaceURI(r.Space),
			Repo:       syntax.DID(r.Repo),
			Collection: syntax.NSID(r.Collection),
			Score:      r.Score,
			Snippet:    r.Snippet,
		}
	}
	return Result{Hits: hits, Cursor: nextCursor(len(hits), limit, offset)}
}

func spaceStrings(in []habitat_syntax.SpaceURI) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = v.String()
	}
	return out
}

func nsidStrings(in []syntax.NSID) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = v.String()
	}
	return out
}

func didStrings(in []syntax.DID) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = v.String()
	}
	return out
}
