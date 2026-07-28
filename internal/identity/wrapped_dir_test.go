package identity

import (
	"context"
	"errors"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

func TestWrappedDirectoryLookupHandle(t *testing.T) {
	baseID := identity.Identity{DID: "did:web:base.example.com", Handle: "alice.example.com"}
	fallbackID := identity.Identity{DID: "did:web:abc123.internal", Handle: "bob.internal"}

	base := identity.NewMockDirectory()
	base.Insert(baseID)
	fallback := identity.NewMockDirectory()
	fallback.Insert(fallbackID)

	wrapped := NewWrappedDirectory(base, fallback)

	t.Run("found in base directory", func(t *testing.T) {
		got, err := wrapped.LookupHandle(t.Context(), baseID.Handle)
		require.NoError(t, err)
		require.Equal(t, baseID.DID, got.DID)
	})

	t.Run("not in base, found in fallback", func(t *testing.T) {
		got, err := wrapped.LookupHandle(t.Context(), fallbackID.Handle)
		require.NoError(t, err)
		require.Equal(t, fallbackID.DID, got.DID)
	})

	t.Run("not found in either", func(t *testing.T) {
		_, err := wrapped.LookupHandle(t.Context(), syntax.Handle("nobody.example.com"))
		require.ErrorIs(t, err, identity.ErrHandleNotFound)
	})
}

func TestWrappedDirectoryLookupDID(t *testing.T) {
	baseID := identity.Identity{DID: "did:web:base.example.com", Handle: "alice.example.com"}
	fallbackID := identity.Identity{DID: "did:web:abc123.internal", Handle: "bob.internal"}

	base := identity.NewMockDirectory()
	base.Insert(baseID)
	fallback := identity.NewMockDirectory()
	fallback.Insert(fallbackID)

	wrapped := NewWrappedDirectory(base, fallback)

	t.Run("found in base directory", func(t *testing.T) {
		got, err := wrapped.LookupDID(t.Context(), baseID.DID)
		require.NoError(t, err)
		require.Equal(t, baseID.Handle, got.Handle)
	})

	t.Run("not in base, found in fallback", func(t *testing.T) {
		got, err := wrapped.LookupDID(t.Context(), fallbackID.DID)
		require.NoError(t, err)
		require.Equal(t, fallbackID.Handle, got.Handle)
	})

	t.Run("not found in either", func(t *testing.T) {
		_, err := wrapped.LookupDID(t.Context(), syntax.DID("did:web:nobody.example.com"))
		require.ErrorIs(t, err, identity.ErrDIDNotFound)
	})
}

func TestWrappedDirectoryLookup(t *testing.T) {
	baseID := identity.Identity{DID: "did:web:base.example.com", Handle: "alice.example.com"}
	fallbackID := identity.Identity{DID: "did:web:abc123.internal", Handle: "bob.internal"}

	base := identity.NewMockDirectory()
	base.Insert(baseID)
	fallback := identity.NewMockDirectory()
	fallback.Insert(fallbackID)

	wrapped := NewWrappedDirectory(base, fallback)

	t.Run("resolves handle via fallback", func(t *testing.T) {
		atid, err := syntax.ParseAtIdentifier(fallbackID.Handle.String())
		require.NoError(t, err)
		got, err := wrapped.Lookup(t.Context(), atid)
		require.NoError(t, err)
		require.Equal(t, fallbackID.DID, got.DID)
	})

	t.Run("resolves DID via fallback", func(t *testing.T) {
		atid, err := syntax.ParseAtIdentifier(fallbackID.DID.String())
		require.NoError(t, err)
		got, err := wrapped.Lookup(t.Context(), atid)
		require.NoError(t, err)
		require.Equal(t, fallbackID.Handle, got.Handle)
	})
}

func TestWrappedDirectoryPurge(t *testing.T) {
	base := identity.NewMockDirectory()
	fallback := identity.NewMockDirectory()
	wrapped := NewWrappedDirectory(base, fallback)

	atid, err := syntax.ParseAtIdentifier("alice.example.com")
	require.NoError(t, err)
	require.NoError(t, wrapped.Purge(t.Context(), atid))
}

// erroringDirectory always returns a non-sentinel error from LookupHandle, to
// verify the wrapped directory doesn't mask unrelated failures (e.g. network
// errors) as "not found" by falling through to the fallback directory.
type erroringDirectory struct {
	*identity.MockDirectory
}

func (e *erroringDirectory) LookupHandle(
	_ context.Context,
	_ syntax.Handle,
) (*identity.Identity, error) {
	return nil, errors.New("simulated network error")
}

func TestWrappedDirectoryLookupHandlePropagatesOtherErrors(t *testing.T) {
	base := &erroringDirectory{MockDirectory: identity.NewMockDirectory()}
	fallback := identity.NewMockDirectory()
	wrapped := NewWrappedDirectory(base, fallback)

	_, err := wrapped.LookupHandle(t.Context(), syntax.Handle("alice.example.com"))
	require.Error(t, err)
	require.NotErrorIs(t, err, identity.ErrHandleNotFound)
}
