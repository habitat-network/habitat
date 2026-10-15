package search

import (
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

func TestProjectMapsFieldsOntoTheSchema(t *testing.T) {
	record := map[string]any{
		"title":     "Launch plan",
		"content":   map[string]any{"text": "ship it", "$type": "x.y.z"},
		"notes":     "low priority aside",
		"status":    "open",
		"tags":      []any{map[string]any{"name": "q3"}, map[string]any{"name": "eng"}},
		"points":    int64(5),
		"dueAt":     "2026-11-01T00:00:00Z",
		"assignees": []any{"did:plc:bob", "not a did", "did:plc:bob"},
		"createdAt": "2026-10-01T00:00:00Z",
	}
	d := Document{Repo: "did:plc:alice"}
	project(&d, record, CollectionConfig{
		Rev: "r1",
		Fields: []Field{
			{
				Path:       "title",
				Searchable: WeightHigh,
				Type:       FieldString,
				Sortable:   true,
				Slot:       "str_0",
			},
			{Path: "content", Searchable: WeightNormal},
			{Path: "notes", Searchable: WeightLow},
			{Path: "status", Type: FieldString, Filterable: true},
			{Path: "tags[].name", Searchable: WeightLow, Type: FieldString, Filterable: true},
			{Path: "points", Type: FieldNumber, Sortable: true, Slot: "num_1"},
			// The wrong type is skipped.
			{Path: "status", Type: FieldNumber, Filterable: true, Slot: "num_2"},
			{Path: "dueAt", Type: FieldDatetime, Filterable: true, Slot: "date_0"},
		},
		Users: []ValueSource{
			MetadataSource{Name: MetadataOwner},
			FieldSource{Path: "assignees[]"},
		},
		Date: []ValueSource{FieldSource{Path: "dueAt"}},
	})
	require.Equal(t, Document{
		Repo:      "did:plc:alice",
		ConfigRev: "r1",
		Title:     "Launch plan",
		Text:      "ship it",
		Extra:     "low priority aside\nq3\neng",
		Facets:    []string{"status=open", "tags[].name=q3", "tags[].name=eng"},
		Numbers:   map[Slot]float64{"num_1": 5},
		Datetimes: map[Slot]time.Time{"date_0": time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
		Strings:   map[Slot]string{"str_0": "Launch plan"},
		Users:     []syntax.DID{"did:plc:alice", "did:plc:bob"},
		Date:      time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
	}, normalizeTimes(d))
}

func TestProjectDefaults(t *testing.T) {
	record := map[string]any{"text": "hello", "createdAt": "2026-10-01T00:00:00Z"}
	d := Document{Repo: "did:plc:alice"}
	project(&d, record, DefaultConfig())
	require.Equal(t, "hello", d.Text)
	require.Equal(t, []syntax.DID{"did:plc:alice"}, d.Users)
	require.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), d.Date.UTC())

	// Empty sources clear users and date.
	d = Document{Repo: "did:plc:alice"}
	project(&d, record, CollectionConfig{Users: []ValueSource{}, Date: []ValueSource{}})
	require.Empty(t, d.Users)
	require.True(t, d.Date.IsZero())
}

// normalizeTimes puts d's times in UTC, so they compare equal.
func normalizeTimes(d Document) Document {
	d.Date = d.Date.UTC()
	for slot, t := range d.Datetimes {
		d.Datetimes[slot] = t.UTC()
	}
	return d
}

func TestParseConfigSkipsUnknownSources(t *testing.T) {
	cfg, err := ParseConfig(map[string]any{
		"users": []any{map[string]any{"$type": "network.habitat.search.config#futureSource"}},
		"date":  map[string]any{"$type": "network.habitat.search.config#futureSource"},
	}, "r1")
	require.NoError(t, err)
	// Each falls back to its default.
	require.Equal(t, CollectionConfig{Rev: "r1"}, cfg)
}

func TestParseConfigRejectsUnusableFields(t *testing.T) {
	for name, field := range map[string]map[string]any{
		"no path":          {"searchable": "high"},
		"unknown weight":   {"path": "a", "searchable": "loud"},
		"no type":          {"path": "a", "filterable": true},
		"wrong slot":       {"path": "a", "type": "number", "sortable": true, "slot": "date_0"},
		"no slot":          {"path": "a", "type": "datetime", "filterable": true},
		"slot not needed":  {"path": "a", "type": "string", "filterable": true, "slot": "str_0"},
		"slot, unfiltered": {"path": "a", "slot": "num_0"},
	} {
		_, err := ParseConfig(map[string]any{"fields": []any{field}}, "r1")
		require.ErrorIs(t, err, ErrInvalidConfig, name)
	}
}
