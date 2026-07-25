package register

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// rewriteTransport routes path-only request URLs to a test server, standing in
// for the OAuth client transport.
type rewriteTransport struct{ base *url.URL }

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = t.base.Scheme
	req.URL.Host = t.base.Host
	return http.DefaultTransport.RoundTrip(req)
}

type fakeClients struct{ base *url.URL }

func (f fakeClients) ClientForSpace(
	context.Context,
	habitat_syntax.SpaceURI,
) (*http.Client, error) {
	return &http.Client{Transport: rewriteTransport(f)}, nil
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
	require.NoError(t, AutoMigrate(db))
	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	reg := New(db, fakeClients{base: base}, fakeSpaces{space}, "https://sap.example")

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
	require.NoError(t, AutoMigrate(db))
	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	reg := New(db, fakeClients{base: base}, fakeSpaces{space}, "https://sap.example")

	require.NoError(t, reg.EnsureRegistered(t.Context(), space))
	require.Equal(t, 1, calls)

	// Second call: already registered, no new HTTP call.
	require.NoError(t, reg.EnsureRegistered(t.Context(), space))
	require.Equal(t, 1, calls)
}

// TestRegistrarWithTx verifies that WithTx returns a registrar scoped to
// the given transaction.
func TestRegistrarWithTx(t *testing.T) {
	t.Parallel()

	db := db_testutil.NewDB(t)
	require.NoError(t, AutoMigrate(db))
	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	reg := New(db, fakeClients{base: &url.URL{}}, fakeSpaces{space}, "https://sap.example")

	tx := db.Begin()
	defer tx.Rollback()
	scoped := reg.WithTx(tx)

	require.NotEqual(t, reg.db, scoped.db)
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
	require.NoError(t, AutoMigrate(db))
	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	reg := New(db, fakeClients{base: base}, fakeSpaces{space}, "https://sap.example")

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
	require.NoError(t, AutoMigrate(db))
	reg := New(db, fakeClients{base: &url.URL{}}, fakeSpaces{}, "https://sap.example")

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
	require.NoError(t, AutoMigrate(db))
	space1 := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	space2 := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s2")
	reg := New(db, fakeClients{base: base}, fakeSpaces{space1, space2}, "https://sap.example")

	// Register only space1.
	require.NoError(t, reg.Register(t.Context(), space1))

	due, err := reg.dueSpaces(t.Context())
	require.NoError(t, err)
	require.Len(t, due, 1)
	require.Equal(t, space2, due[0])
}
