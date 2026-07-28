package main

import "strings"

// ExtractContent flattens every string value found anywhere in a record
// (nested objects and arrays included) into a single space-separated string
// suitable for full-text indexing. Non-string values (numbers, booleans,
// null) are ignored.
func ExtractContent(value map[string]any) string {
	var sb strings.Builder
	walkStrings(value, &sb)
	return strings.TrimSpace(sb.String())
}

func walkStrings(v any, sb *strings.Builder) {
	switch val := v.(type) {
	case string:
		sb.WriteString(val)
		sb.WriteString(" ")
	case map[string]any:
		for _, child := range val {
			walkStrings(child, sb)
		}
	case []any:
		for _, child := range val {
			walkStrings(child, sb)
		}
	}
}
