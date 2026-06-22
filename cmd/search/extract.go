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

// ParseRecordType extracts the NSID collection segment from a space record
// URI of the form "{spaceURI}/{repo}/{collection}/{rkey}", e.g.
// "ats://did:plc:org1/app.space/skey1/did:plc:user1/app.note/rkey1" ->
// "app.note". Returns "" if the URI doesn't have enough segments.
func ParseRecordType(recordURI string) string {
	parts := strings.Split(recordURI, "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-2]
}
