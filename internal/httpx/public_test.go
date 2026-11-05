package httpx

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsPublicAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		"8.8.8.8":                true,
		"2606:4700:4700::1111":   true,
		"127.0.0.1":              false,
		"::1":                    false,
		"10.1.2.3":               false,
		"172.16.0.1":             false,
		"192.168.1.1":            false,
		"169.254.169.254":        false,
		"fe80::1":                false,
		"fd00::1":                false,
		"100.64.0.1":             false,
		"0.0.0.0":                false,
		"::":                     false,
		"224.0.0.1":              false,
		"::ffff:127.0.0.1":       false,
		"::ffff:169.254.169.254": false,
	} {
		t.Run(addr, func(t *testing.T) {
			require.Equal(t, want, IsPublicAddr(netip.MustParseAddr(addr)))
		})
	}
}

func TestPublicClientRefusesLoopback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL, http.NoBody)
	require.NoError(t, err)
	resp, err := NewPublicClient().Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.ErrorIs(t, err, ErrNonPublicAddress)
}

func TestDenyNonPublicRejectsUnparseableAddress(t *testing.T) {
	require.ErrorIs(t, denyNonPublic("tcp", "no-port", nil), ErrNonPublicAddress)
	require.ErrorIs(t, denyNonPublic("tcp", "example.com:443", nil), ErrNonPublicAddress)
	require.NoError(t, denyNonPublic("tcp", "8.8.8.8:443", nil))
}
