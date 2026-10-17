package search

import (
	"slices"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// project fills d's indexed fields from record, its value, as cfg says. It
// reads the record's owner from d.Repo.
//
// A value of the wrong type for its field is skipped. Each searchable
// attribute is capped at [maxTextBytes].
func project(d *Document, record map[string]any, cfg CollectionConfig) {
	d.ConfigRev = cfg.Rev
	d.Users = projectUsers(record, d.Repo, cfg.Users)
	d.Date = projectDate(record, cfg.Date)
	if cfg.Fields == nil {
		d.Text = ExtractText(record)
		return
	}
	var title, body, extra strings.Builder
	for _, f := range cfg.Fields {
		values := lookup(record, f.Path)
		switch f.Searchable {
		case WeightHigh:
			appendText(&title, values)
		case WeightNormal:
			appendText(&body, values)
		case WeightLow:
			appendText(&extra, values)
		}
		if f.Filterable || f.Sortable {
			projectSlot(d, f, values)
		}
	}
	d.Title = capText(title.String())
	d.Text = capText(body.String())
	d.Extra = capText(extra.String())
}

// projectSlot stores the values of a filterable or sortable field: a
// string's as facets, and the first value of the right type in its slot.
func projectSlot(d *Document, f Field, values []any) {
	for _, v := range values {
		switch f.Type {
		case FieldString:
			s, ok := v.(string)
			if !ok {
				continue
			}
			if f.Filterable {
				d.Facets = append(d.Facets, facet(f.Path, s))
			}
			if f.Sortable {
				d.Strings = setSlot(d.Strings, f.Slot, s)
			}
		case FieldNumber:
			if n, ok := number(v); ok {
				d.Numbers = setSlot(d.Numbers, f.Slot, n)
			}
		case FieldDatetime:
			if t, ok := datetime(v); ok {
				d.Datetimes = setSlot(d.Datetimes, f.Slot, t)
			}
		}
	}
}

// facet is the facet stored for a filterable string field's value.
func facet(path, value string) string {
	return path + "=" + value
}

// setSlot sets slot in m unless an earlier value did, making m if needed.
func setSlot[T any](m map[Slot]T, slot Slot, v T) map[Slot]T {
	if m == nil {
		m = map[Slot]T{}
	}
	if _, ok := m[slot]; !ok {
		m[slot] = v
	}
	return m
}

// projectUsers returns the users srcs name, or the owner when srcs is nil.
func projectUsers(record map[string]any, owner syntax.DID, srcs []ValueSource) []syntax.DID {
	if srcs == nil {
		return []syntax.DID{owner}
	}
	var users []syntax.DID
	add := func(did syntax.DID) {
		if !slices.Contains(users, did) {
			users = append(users, did)
		}
	}
	for _, src := range srcs {
		switch src := src.(type) {
		case MetadataSource:
			if src.Name == MetadataOwner {
				add(owner)
			}
		case FieldSource:
			for _, v := range lookup(record, src.Path) {
				s, ok := v.(string)
				if !ok {
					continue
				}
				if did, err := syntax.ParseDID(s); err == nil {
					add(did)
				}
			}
		}
	}
	return users
}

// projectDate returns the date srcs name, or [recordDate] when srcs is nil.
// Records have no date metadata yet, so only a [FieldSource] gives one.
func projectDate(record map[string]any, srcs []ValueSource) time.Time {
	if srcs == nil {
		return recordDate(record)
	}
	for _, src := range srcs {
		if src, ok := src.(FieldSource); ok {
			for _, v := range lookup(record, src.Path) {
				if t, ok := datetime(v); ok {
					return t
				}
			}
		}
	}
	return time.Time{}
}

// lookup returns the values at path in v: a dotted path where a segment
// ending in [] steps into every element of an array.
func lookup(v any, path string) []any {
	values := []any{v}
	for _, seg := range strings.Split(path, ".") {
		key, each := strings.CutSuffix(seg, "[]")
		var next []any
		for _, cur := range values {
			if key != "" {
				m, ok := cur.(map[string]any)
				if !ok {
					continue
				}
				if cur, ok = m[key]; !ok {
					continue
				}
			}
			if !each {
				next = append(next, cur)
				continue
			}
			if arr, ok := cur.([]any); ok {
				next = append(next, arr...)
			}
		}
		values = next
	}
	return values
}

// appendText appends the strings in values to sb, a line each.
func appendText(sb *strings.Builder, values []any) {
	for _, v := range values {
		walk(v, sb)
	}
}

// capText trims text and caps it at [maxTextBytes].
func capText(text string) string {
	return truncateUTF8(strings.TrimSpace(text), maxTextBytes)
}

// number reads v as a number, as records decode them.
func number(v any) (float64, bool) {
	switch n := v.(type) {
	case int64:
		return float64(n), true
	case int:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

// datetime reads v as an atproto datetime.
func datetime(v any) (time.Time, bool) {
	s, ok := v.(string)
	if !ok {
		return time.Time{}, false
	}
	dt, err := syntax.ParseDatetimeLenient(s)
	if err != nil {
		return time.Time{}, false
	}
	return dt.Time(), true
}
