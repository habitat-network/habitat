package pdsclient

import (
	"testing"
)

func TestDoesHandleBelongToDomain(t *testing.T) {
	tests := []struct {
		handle string
		domain string
		want   bool
	}{
		{handle: "test.bsky.app", domain: "bsky.app", want: true},
		{handle: "test.bsky.app", domain: "bsky.social", want: false},
	}

	for _, test := range tests {
		got := doesHandleBelongToDomain(test.handle, test.domain)
		if got != test.want {
			t.Errorf(
				"doesHandleBelongToDomain(%q, %q) = %v, want %v",
				test.handle,
				test.domain,
				got,
				test.want,
			)
		}
	}
}
