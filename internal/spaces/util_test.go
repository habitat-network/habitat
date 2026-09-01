package spaces_test

import (
	"testing"

	"github.com/habitat-network/habitat/internal/spaces"
	"github.com/stretchr/testify/require"
)

// TestMarshalRecord_RejectsNonIntegerFloat pins that MarshalRecord — which
// callers must run over user input before calling PutRecord — rejects values
// that don't conform to the atproto data model.
func TestMarshalRecord_RejectsNonIntegerFloat(t *testing.T) {
	_, err := spaces.MarshalRecord(map[string]any{"x": 0.15})
	require.ErrorIs(t, err, spaces.ErrInvalidRecord)
}
