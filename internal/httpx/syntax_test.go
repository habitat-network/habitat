package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestParseDIDInput(t *testing.T) {
	w := httptest.NewRecorder()
	_, ok := ParseDIDInput(t.Context(), w, "did:web:example.com", "did")
	require.True(t, ok)
	require.Equal(t, 0, w.Body.Len())
	require.Equal(t, http.StatusOK, w.Code)
}

func TestParseDIDInput_Invalid(t *testing.T) {
	w := httptest.NewRecorder()
	_, ok := ParseDIDInput(t.Context(), w, "invalid", "did")
	require.False(t, ok)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.JSONEq(
		t,
		`{"error":"InvalidRequest", "message": "failed to parse did"}`,
		w.Body.String(),
	)
}

func TestParseNSIDInput_Valid(t *testing.T) {
	w := httptest.NewRecorder()
	nsid, ok := ParseNSIDInput(t.Context(), w, "com.example.record", "collection")
	require.True(t, ok)
	require.Equal(t, syntax.NSID("com.example.record"), nsid)
	require.Equal(t, 0, w.Body.Len())
	require.Equal(t, http.StatusOK, w.Code)
}

func TestParseNSIDInput_Invalid(t *testing.T) {
	w := httptest.NewRecorder()
	_, ok := ParseNSIDInput(t.Context(), w, "not.a.valid!!!nsid", "collection")
	require.False(t, ok)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.JSONEq(
		t,
		`{"error":"InvalidRequest", "message": "failed to parse collection"}`,
		w.Body.String(),
	)
}

func TestParseNSIDInput_Empty(t *testing.T) {
	w := httptest.NewRecorder()
	_, ok := ParseNSIDInput(t.Context(), w, "", "collection")
	require.False(t, ok)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.JSONEq(
		t,
		`{"error":"InvalidRequest", "message": "failed to parse collection"}`,
		w.Body.String(),
	)
}

func TestParseSpaceURIInput_Valid(t *testing.T) {
	w := httptest.NewRecorder()
	uri, ok := ParseSpaceURIInput(
		t.Context(),
		w,
		"at://did:web:example.com/space/com.example.space/tidvalue",
		"space",
	)
	require.True(t, ok)
	require.Equal(
		t,
		habitat_syntax.SpaceURI("at://did:web:example.com/space/com.example.space/tidvalue"),
		uri,
	)
	require.Equal(t, 0, w.Body.Len())
	require.Equal(t, http.StatusOK, w.Code)
}

func TestParseSpaceURIInput_Invalid(t *testing.T) {
	w := httptest.NewRecorder()
	_, ok := ParseSpaceURIInput(t.Context(), w, "not-a-valid-uri", "space")
	require.False(t, ok)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.JSONEq(
		t,
		`{"error":"InvalidRequest", "message": "failed to parse space"}`,
		w.Body.String(),
	)
}

func TestParseSpaceURIInput_Empty(t *testing.T) {
	w := httptest.NewRecorder()
	_, ok := ParseSpaceURIInput(t.Context(), w, "", "space")
	require.False(t, ok)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.JSONEq(
		t,
		`{"error":"InvalidRequest", "message": "failed to parse space"}`,
		w.Body.String(),
	)
}

func TestParseSpaceURIInput_InvalidFormat(t *testing.T) {
	w := httptest.NewRecorder()
	_, ok := ParseSpaceURIInput(t.Context(), w, "at://did/space/invalid/format/extra", "space")
	require.False(t, ok)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.JSONEq(
		t,
		`{"error":"InvalidRequest", "message": "failed to parse space"}`,
		w.Body.String(),
	)
}

func TestParseServiceRefInput_Valid(t *testing.T) {
	w := httptest.NewRecorder()
	did, serviceID, ok := ParseServiceRefInput(
		t.Context(),
		w,
		"did:web:sync.example.com#habitat_space_syncer",
		"service identifier",
	)
	require.True(t, ok)
	require.Equal(t, syntax.DID("did:web:sync.example.com"), did)
	require.Equal(t, "habitat_space_syncer", serviceID)
	require.Equal(t, 0, w.Body.Len())
	require.Equal(t, http.StatusOK, w.Code)
}

// TestParseServiceRefInput_BareDID covers the fragment-free form: a bare DID
// names an account, which is served by its personal data server.
func TestParseServiceRefInput_BareDID(t *testing.T) {
	w := httptest.NewRecorder()
	did, serviceID, ok := ParseServiceRefInput(
		t.Context(),
		w,
		"did:plc:someone",
		"service identifier",
	)
	require.True(t, ok)
	require.Equal(t, syntax.DID("did:plc:someone"), did)
	require.Equal(t, "atproto_pds", serviceID)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestParseServiceRefInput_InvalidDID(t *testing.T) {
	w := httptest.NewRecorder()
	_, _, ok := ParseServiceRefInput(
		t.Context(),
		w,
		"not-a-did#habitat_space_syncer",
		"service identifier",
	)
	require.False(t, ok)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.JSONEq(
		t,
		`{"error":"InvalidRequest", "message": "failed to parse service identifier"}`,
		w.Body.String(),
	)
}

func TestParseServiceRefInput_MalformedFragment(t *testing.T) {
	// An empty fragment, a second "#", or whitespace each make the reference
	// ambiguous or unresolvable, so all are rejected rather than resolved.
	for _, input := range []string{
		"did:web:sync.example.com#",
		"did:web:sync.example.com#a#b",
		"did:web:sync.example.com#has space",
	} {
		t.Run(input, func(t *testing.T) {
			w := httptest.NewRecorder()
			_, _, ok := ParseServiceRefInput(
				t.Context(), w, input, "service identifier",
			)
			require.False(t, ok)
			require.Equal(t, http.StatusBadRequest, w.Code)
			require.JSONEq(
				t,
				`{"error":"InvalidRequest", "message": "malformed service identifier"}`,
				w.Body.String(),
			)
		})
	}
}
