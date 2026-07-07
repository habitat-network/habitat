package org

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/core"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// notifyAppServer returns an httptest server implementing notifyApp that records
// every request it receives.
func notifyAppServer(t *testing.T, wg *sync.WaitGroup) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// deferred so a failing require below (which calls Goexit) still releases
		// the wait instead of hanging the test.
		defer wg.Done()
		body, _ := io.ReadAll(r.Body)
		var input habitat.NetworkHabitatOrgNotifyAppInput
		require.NoError(t, json.Unmarshal(body, &input))
		token, _, err := jwt.NewParser().
			ParseUnverified(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), jwt.MapClaims{})
		require.NoError(t, err)
		// Service auth carries the org DID in the issuer claim, not the subject.
		iss, err := token.Claims.GetIssuer()
		require.NoError(t, err)
		require.Equal(t, input.Org, iss)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testHive(t *testing.T) hive.Hive {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"))
	require.NoError(t, err)
	h, err := hive.NewHive("id.example.com", "pear.example.com", db)
	require.NoError(t, err)
	return h
}

func TestNotifyApps(t *testing.T) {
	var wg sync.WaitGroup
	srv1 := notifyAppServer(t, &wg)
	srv2 := notifyAppServer(t, &wg)
	h := testHive(t)
	id, err := h.MintOrgIdentity(t.Context(), "org.handle")
	require.NoError(t, err)
	notifier := NewNotifier([]string{srv1.URL, srv2.URL}, h)
	wg.Add(2)
	notifier.NotifyApps(t.Context(), id.DID)
	wg.Wait()
}

type testOrgLister struct {
	ids []syntax.DID
}

func (t testOrgLister) ListOrgs(ctx context.Context) ([]core.Org, error) {
	orgs := make([]core.Org, 0, len(t.ids))
	for _, id := range t.ids {
		orgs = append(orgs, &orgImpl{orgID: id})
	}
	return orgs, nil
}

func TestBootstrapNotifications(t *testing.T) {
	var wg sync.WaitGroup
	srv1 := notifyAppServer(t, &wg)
	srv2 := notifyAppServer(t, &wg)
	h := testHive(t)
	id1, err := h.MintOrgIdentity(t.Context(), "org1.handle")
	require.NoError(t, err)
	id2, err := h.MintOrgIdentity(t.Context(), "org2.handle")
	require.NoError(t, err)

	notifier := NewNotifier([]string{srv1.URL, srv2.URL}, h)

	wg.Add(4)
	require.NoError(t, BootstrapNotifications(
		t.Context(),
		notifier, testOrgLister{ids: []syntax.DID{id1.DID, id2.DID}}))
	wg.Wait()
}
