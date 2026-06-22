package main

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type searchDocument struct {
	URI        string    `gorm:"column:uri;primaryKey"`
	SpaceURI   string    `gorm:"column:space_uri;index"`
	OrgDID     string    `gorm:"column:org_did;index"`
	RecordType string    `gorm:"column:record_type"`
	Content    string    `gorm:"column:content"`
	UpdatedAt  time.Time `gorm:"column:updated_at"`
}

func (searchDocument) TableName() string { return "search_documents" }

type postgresFTSIndex struct {
	db *gorm.DB
}

var _ Index = (*postgresFTSIndex)(nil)

func newPostgresFTSIndex(db *gorm.DB) (*postgresFTSIndex, error) {
	if err := db.AutoMigrate(&searchDocument{}); err != nil {
		return nil, fmt.Errorf("migrate search_documents: %w", err)
	}
	if err := db.Exec(`
		ALTER TABLE search_documents
		  ADD COLUMN IF NOT EXISTS tsv tsvector
		  GENERATED ALWAYS AS (to_tsvector('english', content)) STORED
	`).Error; err != nil {
		return nil, fmt.Errorf("add tsv column: %w", err)
	}
	if err := db.Exec(`
		CREATE INDEX IF NOT EXISTS search_documents_tsv_idx
		  ON search_documents USING GIN (tsv)
	`).Error; err != nil {
		return nil, fmt.Errorf("create tsv index: %w", err)
	}
	return &postgresFTSIndex{db: db}, nil
}

func (idx *postgresFTSIndex) Upsert(ctx context.Context, doc Document) error {
	row := searchDocument{
		URI:        doc.URI,
		SpaceURI:   doc.SpaceURI,
		OrgDID:     doc.OrgDID,
		RecordType: doc.RecordType,
		Content:    doc.Content,
		UpdatedAt:  doc.UpdatedAt,
	}
	return idx.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "uri"}},
		UpdateAll: true,
	}).Create(&row).Error
}

func (idx *postgresFTSIndex) Delete(ctx context.Context, uri string) error {
	return idx.db.WithContext(ctx).Delete(&searchDocument{}, "uri = ?", uri).Error
}

type ftsRow struct {
	URI        string
	SpaceURI   string
	RecordType string
	Snippet    string
	Rank       float64
}

func (idx *postgresFTSIndex) Query(ctx context.Context, params QueryParams) (QueryResult, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 25
	}
	offset := 0
	if params.Cursor != "" {
		parsed, err := strconv.Atoi(params.Cursor)
		if err != nil {
			return QueryResult{}, fmt.Errorf("invalid cursor %q: %w", params.Cursor, err)
		}
		offset = parsed
	}

	var rows []ftsRow
	err := idx.db.WithContext(ctx).Raw(`
		SELECT uri, space_uri, record_type,
		       ts_rank(tsv, query) AS rank,
		       ts_headline('english', content, query) AS snippet
		FROM search_documents, websearch_to_tsquery('english', ?) query
		WHERE org_did = ? AND tsv @@ query
		ORDER BY rank DESC, uri ASC
		LIMIT ? OFFSET ?
	`, params.QueryText, params.OrgDID, limit, offset).Scan(&rows).Error
	if err != nil {
		return QueryResult{}, fmt.Errorf("query search_documents: %w", err)
	}

	results := make([]Result, len(rows))
	for i, row := range rows {
		results[i] = Result{
			URI:        row.URI,
			SpaceURI:   row.SpaceURI,
			RecordType: row.RecordType,
			Snippet:    row.Snippet,
			Rank:       row.Rank,
		}
	}
	nextCursor := ""
	if len(rows) == limit {
		nextCursor = strconv.Itoa(offset + limit)
	}
	return QueryResult{Results: results, NextCursor: nextCursor}, nil
}
