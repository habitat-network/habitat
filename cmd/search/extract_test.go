package main

import "testing"

func TestExtractContent_FlattensNestedStrings(t *testing.T) {
	value := map[string]any{
		"title": "Quarterly Budget",
		"body":  "Review the numbers",
		"tags":  []any{"finance", "q3"},
		"meta": map[string]any{
			"author": "alice",
		},
		"count": 42, // non-string values are ignored
	}

	got := ExtractContent(value)
	for _, want := range []string{"Quarterly Budget", "Review the numbers", "finance", "q3", "alice"} {
		if !contains(got, want) {
			t.Errorf("ExtractContent() = %q, want to contain %q", got, want)
		}
	}
}

func TestExtractContent_EmptyRecord(t *testing.T) {
	if got := ExtractContent(map[string]any{}); got != "" {
		t.Errorf("ExtractContent(empty) = %q, want empty string", got)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestParseRecordType_ParsesCollectionSegment(t *testing.T) {
	uri := "ats://did:plc:org1/app.space/skey1/did:plc:user1/app.note/rkey1"
	if got := ParseRecordType(uri); got != "app.note" {
		t.Errorf("ParseRecordType(%q) = %q, want %q", uri, got, "app.note")
	}
}

func TestParseRecordType_ShortURIReturnsEmpty(t *testing.T) {
	if got := ParseRecordType("not-a-uri"); got != "" {
		t.Errorf("ParseRecordType(short) = %q, want empty string", got)
	}
}
