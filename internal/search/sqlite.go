package search

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"gorm.io/gorm"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// sqliteFTSSchema keeps an FTS5 index in step with search_documents. The index
// is an external-content table: it stores only the tokens and reads text back
// from search_documents by rowid, so the text isn't stored twice. Triggers
// mirror every insert, update and delete of search_documents into it.
var sqliteFTSSchema = []string{
	`CREATE VIRTUAL TABLE IF NOT EXISTS search_documents_fts USING fts5(
		body,
		content='search_documents',
		content_rowid='rowid',
		tokenize='unicode61 remove_diacritics 2'
	)`,
	`CREATE TRIGGER IF NOT EXISTS search_documents_ai AFTER INSERT ON search_documents BEGIN
		INSERT INTO search_documents_fts(rowid, body) VALUES (new.rowid, new.body);
	END`,
	`CREATE TRIGGER IF NOT EXISTS search_documents_ad AFTER DELETE ON search_documents BEGIN
		INSERT INTO search_documents_fts(search_documents_fts, rowid, body)
		VALUES ('delete', old.rowid, old.body);
	END`,
	`CREATE TRIGGER IF NOT EXISTS search_documents_au AFTER UPDATE ON search_documents BEGIN
		INSERT INTO search_documents_fts(search_documents_fts, rowid, body)
		VALUES ('delete', old.rowid, old.body);
		INSERT INTO search_documents_fts(rowid, body) VALUES (new.rowid, new.body);
	END`,
	// Index rows that predate the triggers.
	`INSERT INTO search_documents_fts(search_documents_fts) VALUES ('rebuild')`,
}

type sqliteIndex struct {
	db *gorm.DB
}

var _ Index = (*sqliteIndex)(nil)

func (i *sqliteIndex) Put(ctx context.Context, docs ...Document) error {
	return putDocuments(ctx, i.db, docs)
}

func (i *sqliteIndex) Delete(ctx context.Context, uris ...habitat_syntax.SpaceRecordURI) error {
	return deleteDocuments(ctx, i.db, uris)
}

func (i *sqliteIndex) DeleteSpace(ctx context.Context, space habitat_syntax.SpaceURI) error {
	return deleteSpaceDocuments(ctx, i.db, space)
}

type searchRow struct {
	URI        string
	Space      string
	Repo       string
	Collection string
	Score      float64
	Snippet    string
}

func (i *sqliteIndex) Search(ctx context.Context, q Query) (Result, error) {
	limit, offset, err := pageParams(q)
	if err != nil {
		return Result{}, err
	}
	match := ftsMatch(q.Text)
	if match == "" || len(q.Spaces) == 0 {
		return Result{}, nil
	}

	sql := `
		SELECT d.uri, d.space, d.repo, d.collection,
		       -bm25(search_documents_fts) AS score,
		       snippet(search_documents_fts, 0, '<mark>', '</mark>', '…', 16) AS snippet
		FROM search_documents_fts
		JOIN search_documents d ON d.rowid = search_documents_fts.rowid
		WHERE search_documents_fts MATCH ? AND d.space IN ?`
	args := []any{match, spaceStrings(q.Spaces)}
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

var nonWord = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// ftsMatch turns free-form user input into an FTS5 MATCH expression that
// requires every word, quoting each so operators and punctuation in the input
// can't be read as FTS5 syntax. It returns "" when the input has no words.
func ftsMatch(text string) string {
	words := nonWord.Split(text, -1)
	quoted := make([]string, 0, len(words))
	for _, w := range words {
		if w != "" {
			quoted = append(quoted, `"`+w+`"`)
		}
	}
	return strings.Join(quoted, " ")
}
