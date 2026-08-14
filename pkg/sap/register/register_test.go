package register

import (
	"context"
	"encoding/json"
	"errors"
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

type fakeSpacesErr struct{ err error }

func (f fakeSpacesErr) Spaces(context.Context) ([]habitat_syntax.SpaceURI, error) {
	return nil, f.err
}

type fakeClientsErr struct{ err error }

func (f fakeClientsErr) ClientForSpace(
	context.Context,
	habitat_syntax.SpaceURI,
) (*atclient.APIClient, error) {
	return nil, f.err
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

// TestRegistrarWithTx verifies that WithTx returns a registrar scoped to
// the given transaction.
func TestRegistrarWithTx(t *testing.T) {
	t.Parallel()

	db := db_testutil.NewDB(t)
	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	reg, err := New(db, fakeClients{base: &url.URL{}}, fakeSpaces{space}, "https://sap.example")
	require.NoError(t, err)

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

// TestRegistrarDueSpacesListError verifies dueSpaces surfaces an error from
// listing spaces.
func TestRegistrarDueSpacesListError(t *testing.T) {
	t.Parallel()

	db := db_testutil.NewDB(t)
	reg, err := New(
		db, fakeClients{base: &url.URL{}}, fakeSpacesErr{err: errors.New("boom")}, "https://sap.example",
	)
	require.NoError(t, err)

	_, err = reg.dueSpaces(t.Context())
	require.Error(t, err)
}

// TestRegistrarSweepListSpacesError verifies sweep swallows (logs) an error
// listing spaces rather than panicking.
func TestRegistrarSweepListSpacesError(t *testing.T) {
	t.Parallel()

	db := db_testutil.NewDB(t)
	reg, err := New(
		db, fakeClients{base: &url.URL{}}, fakeSpacesErr{err: errors.New("boom")}, "https://sap.example",
	)
	require.NoError(t, err)

	reg.sweep(t.Context())

	var count int64
	require.NoError(t, db.Model(&registration{}).Count(&count).Error)
	require.Equal(t, int64(0), count)
}

// TestRegistrarSweepRegisterError verifies sweep logs a per-space Register
// failure and continues rather than aborting the whole sweep.
func TestRegistrarSweepRegisterError(t *testing.T) {
	t.Parallel()

	db := db_testutil.NewDB(t)
	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	reg, err := New(
		db, fakeClientsErr{err: errors.New("no client")}, fakeSpaces{space}, "https://sap.example",
	)
	require.NoError(t, err)

	reg.sweep(t.Context())

	var count int64
	require.NoError(t, db.Model(&registration{}).Count(&count).Error)
	require.Equal(t, int64(0), count)
}

// TestRegistrarRegisterClientError verifies Register surfaces a
// ClientForSpace failure.
func TestRegistrarRegisterClientError(t *testing.T) {
	t.Parallel()

	db := db_testutil.NewDB(t)
	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	reg, err := New(
		db, fakeClientsErr{err: errors.New("no client")}, fakeSpaces{space}, "https://sap.example",
	)
	require.NoError(t, err)

	require.Error(t, reg.Register(t.Context(), space))
}

// TestRegistrarRegisterHTTPError verifies Register surfaces a registerNotify
// HTTP failure.
func TestRegistrarRegisterHTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	base, err := url.Parse(srv.URL)
	require.NoError(t, err)

	db := db_testutil.NewDB(t)
	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	reg, err := New(db, fakeClients{base: base}, fakeSpaces{space}, "https://sap.example")
	require.NoError(t, err)

	require.Error(t, reg.Register(t.Context(), space))
}

// TestRegistrarRegisterBadExpiresAt verifies Register surfaces a malformed
// expiresAt from the host.
func TestRegistrarRegisterBadExpiresAt(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceRegisterNotifyOutput{
			ExpiresAt: "not-a-time",
		})
	}))
	t.Cleanup(srv.Close)
	base, err := url.Parse(srv.URL)
	require.NoError(t, err)

	db := db_testutil.NewDB(t)
	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	reg, err := New(db, fakeClients{base: base}, fakeSpaces{space}, "https://sap.example")
	require.NoError(t, err)

	require.Error(t, reg.Register(t.Context(), space))
}

// TestRegistrarEnsureRegisteredDBError verifies EnsureRegistered surfaces a
// lookup error that isn't a plain "not found".
func TestRegistrarEnsureRegisteredDBError(t *testing.T) {
	t.Parallel()

	db := db_testutil.NewDB(t)
	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	reg, err := New(db, fakeClients{base: &url.URL{}}, fakeSpaces{space}, "https://sap.example")
	require.NoError(t, err)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	require.Error(t, reg.EnsureRegistered(t.Context(), space))
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
