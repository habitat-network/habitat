package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// ConfigCollection holds an org's search config records, one per collection,
// keyed by the collection's NSID, in the org's members space.
const ConfigCollection = syntax.NSID("network.habitat.search.config")

// ConfigSource says which collections are indexed and searched for an org,
// and how. [github.com/habitat-network/habitat/internal/searchconfig.Store]
// implements it: the hard-coded defaults merged with the org's
// [ConfigCollection] records.
type ConfigSource interface {
	// Configs returns every collection indexed for org, keyed by NSID.
	Configs(ctx context.Context, org syntax.DID) (map[syntax.NSID]CollectionConfig, error)
}

// Weight is how much a searchable field counts in ranking. Each weight is a
// searchable attribute of the fixed schema, ranked in this order.
type Weight string

const (
	// WeightHigh fields fill title.
	WeightHigh Weight = "high"
	// WeightNormal fields fill body, which is chunked.
	WeightNormal Weight = "normal"
	// WeightLow fields fill extra.
	WeightLow Weight = "low"
)

// FieldType is the type a field's values are read as.
type FieldType string

const (
	FieldString   FieldType = "string"
	FieldNumber   FieldType = "number"
	FieldDatetime FieldType = "datetime"
)

// Slot is a typed attribute of the fixed schema that a filterable or
// sortable number, datetime or string field is stored in. A slot means
// something different in each collection, so filters and sorts on one name
// the collection.
type Slot string

// The slots, by type.
var (
	numberSlots   = []Slot{"num_0", "num_1", "num_2", "num_3"}
	datetimeSlots = []Slot{"date_0", "date_1", "date_2", "date_3"}
	stringSlots   = []Slot{"str_0", "str_1"}
)

// Field is how one field of a collection's records is indexed.
type Field struct {
	// Path is a dotted path into the record value, with [] for each element
	// of an array, such as "tags[].name".
	Path string
	// Searchable, when set, makes the field's text searchable with that
	// weight. A field with objects or arrays under it contributes every string
	// in them.
	Searchable Weight
	// Type is what the field's values are read as for Filterable and Sortable.
	// A value of another type is skipped.
	Type FieldType
	// Filterable string fields can be filtered on by equality; number and
	// datetime fields also by comparison, and need a Slot.
	Filterable bool
	// Sortable fields need a Slot.
	Sortable bool
	// Slot is where a number, datetime or sortable string field is stored. It
	// is fixed in the config, so reordering fields doesn't move data.
	Slot Slot
}

// ValueSource is where a config takes a value from: a [FieldSource] or a
// [MetadataSource].
type ValueSource interface {
	isValueSource()
}

// FieldSource takes values from a path into the record value, as
// [Field.Path].
type FieldSource struct {
	Path string
}

// MetadataSource takes a value from the record's metadata rather than its
// value.
type MetadataSource struct {
	Name MetadataName
}

func (FieldSource) isValueSource()    {}
func (MetadataSource) isValueSource() {}

// MetadataName names a [MetadataSource].
type MetadataName string

// MetadataOwner is the DID of the repo the record is in.
const MetadataOwner MetadataName = "owner"

// CollectionConfig is how one collection's records are indexed.
type CollectionConfig struct {
	// Rev identifies this version of the config, and is stored on every
	// document projected with it.
	Rev string
	// Fields are the indexed fields. When nil, every string in the record is
	// searchable with [WeightNormal], and nothing is filterable or sortable.
	Fields []Field
	// Users are where the people a record is about come from. Nil means the
	// record's owner; empty means none.
	Users []ValueSource
	// Date is where the record's date comes from, as one source. Nil means the
	// record's updatedAt, else its createdAt; empty means none.
	Date []ValueSource
	// NoBlobs leaves the text of the blobs a record references unindexed.
	NoBlobs bool
}

// DefaultConfigRev is the Rev of [DefaultConfig]. Change it when
// DefaultConfig changes.
const DefaultConfigRev = "default-1"

// DefaultConfig is the config of a collection whose config sets nothing.
func DefaultConfig() CollectionConfig {
	return CollectionConfig{Rev: DefaultConfigRev}
}

// ErrInvalidConfig is returned for a config record that can't be used.
var ErrInvalidConfig = errors.New("invalid search config")

// configRecord is a [ConfigCollection] record. Unions are decoded by
// [decodeSource], so a source of a kind pear doesn't know yet is skipped.
type configRecord struct {
	Fields []struct {
		Path       string    `json:"path"`
		Searchable Weight    `json:"searchable"`
		Type       FieldType `json:"type"`
		Filterable bool      `json:"filterable"`
		Sortable   bool      `json:"sortable"`
		Slot       Slot      `json:"slot"`
	} `json:"fields"`
	Blobs *bool `json:"blobs"`
}

// ParseConfig reads the value of a [ConfigCollection] record into the
// config with rev rev. What the record omits keeps its [DefaultConfig]
// value.
func ParseConfig(value map[string]any, rev string) (CollectionConfig, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return CollectionConfig{}, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	var rec configRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return CollectionConfig{}, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	// users and date tell a null, which clears them, from a missing key,
	// which keeps the default.
	var present map[string]json.RawMessage
	if err := json.Unmarshal(raw, &present); err != nil {
		return CollectionConfig{}, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	cfg := CollectionConfig{Rev: rev}
	if users, ok := present["users"]; ok {
		var srcs []json.RawMessage
		if err := json.Unmarshal(users, &srcs); err != nil {
			return CollectionConfig{}, fmt.Errorf("%w: users: %w", ErrInvalidConfig, err)
		}
		cfg.Users = []ValueSource{}
		for _, u := range srcs {
			if src, ok := decodeSource(u); ok {
				cfg.Users = append(cfg.Users, src)
			}
		}
		if len(srcs) > 0 && len(cfg.Users) == 0 {
			// Only kinds pear doesn't know: fall back to the default.
			cfg.Users = nil
		}
	}
	if date, ok := present["date"]; ok {
		cfg.Date = []ValueSource{}
		if string(date) != "null" {
			if src, ok := decodeSource(date); ok {
				cfg.Date = append(cfg.Date, src)
			} else {
				// An unknown kind falls back to the default.
				cfg.Date = nil
			}
		}
	}
	if rec.Blobs != nil {
		cfg.NoBlobs = !*rec.Blobs
	}
	if rec.Fields != nil {
		cfg.Fields = []Field{}
	}
	for _, f := range rec.Fields {
		cfg.Fields = append(cfg.Fields, Field(f))
	}
	if err := cfg.validate(); err != nil {
		return CollectionConfig{}, err
	}
	return cfg, nil
}

// decodeSource decodes one value source, reporting false for a kind pear
// doesn't know.
func decodeSource(raw json.RawMessage) (ValueSource, bool) {
	var src struct {
		Type string       `json:"$type"`
		Path string       `json:"path"`
		Name MetadataName `json:"name"`
	}
	if err := json.Unmarshal(raw, &src); err != nil {
		return nil, false
	}
	switch strings.TrimPrefix(src.Type, ConfigCollection.String()) {
	case "#fieldSource":
		if src.Path != "" {
			return FieldSource{Path: src.Path}, true
		}
	case "#metadataSource":
		if src.Name == MetadataOwner {
			return MetadataSource{Name: src.Name}, true
		}
	}
	return nil, false
}

// validate checks that every field is usable, and that no two share a slot.
func (c CollectionConfig) validate() error {
	used := map[Slot]string{}
	for _, f := range c.Fields {
		if f.Path == "" {
			return fmt.Errorf("%w: a field has no path", ErrInvalidConfig)
		}
		switch f.Searchable {
		case "", WeightHigh, WeightNormal, WeightLow:
		default:
			return fmt.Errorf(
				"%w: field %s: unknown weight %q",
				ErrInvalidConfig,
				f.Path,
				f.Searchable,
			)
		}
		if !f.Filterable && !f.Sortable {
			if f.Slot != "" {
				return fmt.Errorf("%w: field %s has a slot but isn't filterable or sortable",
					ErrInvalidConfig, f.Path)
			}
			continue
		}
		var slots []Slot
		switch f.Type {
		case FieldNumber:
			slots = numberSlots
		case FieldDatetime:
			slots = datetimeSlots
		case FieldString:
			if !f.Sortable {
				// Filterable strings are stored as facets.
				if f.Slot != "" {
					return fmt.Errorf("%w: field %s: only sortable strings take a slot",
						ErrInvalidConfig, f.Path)
				}
				continue
			}
			slots = stringSlots
		default:
			return fmt.Errorf("%w: field %s: a filterable or sortable field needs a type",
				ErrInvalidConfig, f.Path)
		}
		if !slices.Contains(slots, f.Slot) {
			return fmt.Errorf("%w: field %s: a %s field needs one of the slots %v",
				ErrInvalidConfig, f.Path, f.Type, slots)
		}
		if other, ok := used[f.Slot]; ok {
			return fmt.Errorf("%w: fields %s and %s share slot %s",
				ErrInvalidConfig, other, f.Path, f.Slot)
		}
		used[f.Slot] = f.Path
	}
	return nil
}
