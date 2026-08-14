package register

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

type fakeClients struct{ base *url.URL }

func (f fakeClients) ClientForSpace(
	context.Context,
	habitat_syntax.SpaceURI,
) (*atclient.APIClient, error) {
	return &atclient.APIClient{Client: http.DefaultClient, Host: f.base.String()}, nil
}

type fakeSpaces []habitat_syntax.SpaceURI

func (f fakeSpaces) Spaces(context.Context) ([]habitat_syntax.SpaceURI, error) {
	return f, nil
}

// TestRegistrarRegistersDueSpaces covers the happy path: an unregistered space
// is registered with the host and its expiry recorded; a fresh registration is
// not re-registered on the next sweep.
func TestRegistrarRegistersDueSpaces(t *testing.T) {
	t.Parallel()

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/xrpc/network.habitat.space.registerNotify", r.URL.Path)
		calls++
		var in habitat.NetworkHabitatSpaceRegisterNotifyInput
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		require.Equal(t, "https://sap.example", in.Endpoint)
		_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceRegisterNotifyOutput{
			ExpiresAt: time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
		})
	}))
	t.Cleanup(srv.Close)
	base, err := url.Parse(srv.URL)
	require.NoError(t, err)

	db := db_testutil.NewDB(t)
	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	reg, err := New(db, fakeClients{base: base}, fakeSpaces{space}, "https://sap.example")
	require.NoError(t, err)

	reg.sweep(t.Context())
	require.Equal(t, 1, calls)

	var row registration
	require.NoError(t, db.First(&row, "space = ?", space).Error)
	require.True(t, row.ExpiresAt.After(time.Now()))

	// Still fresh: nothing to do on the next sweep.
	reg.sweep(t.Context())
	require.Equal(t, 1, calls)
}

// TestRegistrarEnsureRegisteredAlreadyTracked verifies that EnsureRegistered
// is a no-op when the space already has a registration.
func TestRegistrarEnsureRegisteredAlreadyTracked(t *testing.T) {
	t.Parallel()

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceRegisterNotifyOutput{
			ExpiresAt: time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
		})
	}))
	t.Cleanup(srv.Close)
	base, _ := url.Parse(srv.URL)

	db := db_testutil.NewDB(t)
	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	reg, err := New(db, fakeClients{base: base}, fakeSpaces{space}, "https://sap.example")
	require.NoError(t, err)

	require.NoError(t, reg.EnsureRegistered(t.Context(), space))
	require.Equal(t, 1, calls)

	// Second call: already registered, no new HTTP call.
	require.NoError(t, reg.EnsureRegistered(t.Context(), space))
	require.Equal(t, 1, calls)
}

// TestRegistrarDropSpace verifies that DropSpace removes the registration.
func TestRegistrarDropSpace(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceRegisterNotifyOutput{
			ExpiresAt: time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
		})
	}))
	t.Cleanup(srv.Close)
	base, _ := url.Parse(srv.URL)

	db := db_testutil.NewDB(t)
	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	reg, err := New(db, fakeClients{base: base}, fakeSpaces{space}, "https://sap.example")
	require.NoError(t, err)

	require.NoError(t, reg.Register(t.Context(), space))

	var count int64
	require.NoError(t, db.Model(&registration{}).Where("space = ?", space).Count(&count).Error)
	require.Equal(t, int64(1), count)

	require.NoError(t, reg.DropSpace(t.Context(), space))

	require.NoError(t, db.Model(&registration{}).Where("space = ?", space).Count(&count).Error)
	require.Equal(t, int64(0), count)
}

// TestRegistrarDueSpacesEmpty verifies dueSpaces returns nil for no spaces.
func TestRegistrarDueSpacesEmpty(t *testing.T) {
	t.Parallel()

	db := db_testutil.NewDB(t)
	reg, err := New(db, fakeClients{base: &url.URL{}}, fakeSpaces{}, "https://sap.example")
	require.NoError(t, err)

	due, err := reg.dueSpaces(t.Context())
	require.NoError(t, err)
	require.Nil(t, due)
}

// TestRegistrarDueSpacesFiltersFresh verifies that dueSpaces only returns
// spaces whose registrations are expired or missing.
func TestRegistrarDueSpacesFiltersFresh(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceRegisterNotifyOutput{
			ExpiresAt: time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
		})
	}))
	t.Cleanup(srv.Close)
	base, _ := url.Parse(srv.URL)

	db := db_testutil.NewDB(t)
	space1 := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	space2 := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s2")
	reg, err := New(db, fakeClients{base: base}, fakeSpaces{space1, space2}, "https://sap.example")
	require.NoError(t, err)

	// Register only space1.
	require.NoError(t, reg.Register(t.Context(), space1))

	due, err := reg.dueSpaces(t.Context())
	require.NoError(t, err)
	require.Len(t, due, 1)
	require.Equal(t, space2, due[0])
}

// TestRegistrarRun verifies Run performs an initial sweep and returns
// promptly once ctx is done.
func TestRegistrarRun(t *testing.T) {
	t.Parallel()

	db := db_testutil.NewDB(t)
	reg, err := New(db, fakeClients{base: &url.URL{}}, fakeSpaces{}, "https://sap.example")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	reg.Run(ctx)
}
