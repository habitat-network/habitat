package opensocial_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
)

// spanAttrs returns the attributes of the one ended span named name.
func spanAttrs(
	t *testing.T,
	rec *tracetest.SpanRecorder,
	name string,
) map[attribute.Key]attribute.Value {
	t.Helper()
	var found []sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.Name() == name {
			found = append(found, s)
		}
	}
	require.Len(t, found, 1, name)
	out := map[attribute.Key]attribute.Value{}
	for _, a := range found[0].Attributes() {
		out[a.Key] = a.Value
	}
	return out
}

func TestStoreTracesCommunityOperations(t *testing.T) {
	// Package tracers bind to the first global provider, so this test is
	// the only one in the package to set one.
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))

	s := opensocial_testutil.NewTestStore(t)
	creator := syntax.DID("did:plc:creator")
	stranger := syntax.DID("did:plc:stranger")
	orgDIDStr, err := s.NewOrg(t.Context(), "traced", creator)
	require.NoError(t, err)
	org := syntax.DID(orgDIDStr)

	ok, err := s.CheckAction(t.Context(), org, stranger, opensocial.ActionInvite)
	require.NoError(t, err)
	require.False(t, ok)
	attrs := spanAttrs(t, rec, "opensocial.CheckAction")
	require.Equal(t, org.String(), attrs["habitat.org"].AsString())
	require.Equal(t, stranger.String(), attrs["habitat.opensocial.member"].AsString())
	require.Equal(t, string(opensocial.ActionInvite), attrs["habitat.opensocial.action"].AsString())
	require.False(t, attrs["habitat.opensocial.allowed"].AsBool())

	err = s.RevokeInvite(t.Context(), org, "missing")
	require.ErrorIs(t, err, opensocial.ErrInviteNotFound)
	for _, span := range rec.Ended() {
		if span.Name() == "opensocial.RevokeInvite" {
			require.Equal(t, codes.Error, span.Status().Code)
		}
	}
	require.Equal(
		t,
		"missing",
		spanAttrs(t, rec, "opensocial.RevokeInvite")["habitat.opensocial.invite.id"].AsString(),
	)
}
