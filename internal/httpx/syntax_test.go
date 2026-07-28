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
		"ats://did:web:example.com/com.example.space/tidvalue",
		"space",
	)
	require.True(t, ok)
	require.Equal(
		t,
		habitat_syntax.SpaceURI("ats://did:web:example.com/com.example.space/tidvalue"),
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
	_, ok := ParseSpaceURIInput(t.Context(), w, "ats://did/invalid/space/format/extra", "space")
	require.False(t, ok)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.JSONEq(
		t,
		`{"error":"InvalidRequest", "message": "failed to parse space"}`,
		w.Body.String(),
	)
}
