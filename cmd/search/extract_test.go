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
