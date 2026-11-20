package identity

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

func TestNewDirectoryResolvesPLCAgainstGivenURL(t *testing.T) {
	did := syntax.DID("did:plc:abcdefghijklmnopqrstuvwx")
	var requested string
	plc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          did.String(),
			"alsoKnownAs": []string{"at://alice.example.com"},
			"service": []map[string]string{{
				"id":              "#atproto_pds",
				"type":            "AtprotoPersonalDataServer",
				"serviceEndpoint": "https://pds.example.com",
			}},
		})
	}))
	defer plc.Close()

	ident, err := NewDirectory(plc.URL).LookupDID(t.Context(), did)
	require.NoError(t, err)
	require.Equal(t, "/"+did.String(), requested)
	require.Equal(t, "https://pds.example.com", ident.PDSEndpoint())
}

func TestNewDirectoryDefaultsToPublicDirectory(t *testing.T) {
	require.NotNil(t, NewDirectory(""))
}
