package search

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

type postgresIndex struct {
	db *gorm.DB
}

var _ Index = (*postgresIndex)(nil)

func (i *postgresIndex) Put(ctx context.Context, docs ...Document) error {
	return putDocuments(ctx, i.db, docs)
}

func (i *postgresIndex) Delete(ctx context.Context, uris ...habitat_syntax.SpaceRecordURI) error {
	return deleteDocuments(ctx, i.db, uris)
}

func (i *postgresIndex) DeleteSpace(ctx context.Context, space habitat_syntax.SpaceURI) error {
	return deleteSpaceDocuments(ctx, i.db, space)
}

func (i *postgresIndex) Search(ctx context.Context, q Query) (Result, error) {
	limit, offset, err := pageParams(q)
	if err != nil {
		return Result{}, err
	}
	if len(q.Spaces) == 0 {
		return Result{}, nil
	}

	// The WHERE clause's to_tsvector expression must match the one pear's
	// search_fts migration indexes, or Postgres can't use the index.
	sql := `
		SELECT d.uri, d.space, d.repo, d.collection,
		       ts_rank(to_tsvector('simple', d.body), query) AS score,
		       ts_headline('simple', d.body, query,
		         'StartSel=<mark>,StopSel=</mark>,MaxWords=20,MinWords=8') AS snippet
		FROM search_documents d, websearch_to_tsquery('simple', ?) query
		WHERE to_tsvector('simple', d.body) @@ query AND d.space IN ?`
	args := []any{q.Text, spaceStrings(q.Spaces)}
	if len(q.Collections) > 0 {
		sql += " AND d.collection IN ?"
		args = append(args, nsidStrings(q.Collections))
	}
	if len(q.Repos) > 0 {
		sql += " AND d.repo IN ?"
		args = append(args, didStrings(q.Repos))
	}
	sql += " ORDER BY score DESC, d.uri ASC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	var rows []searchRow
	if err := i.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return Result{}, fmt.Errorf("search documents: %w", err)
	}
	return toResult(rows, limit, offset), nil
}
