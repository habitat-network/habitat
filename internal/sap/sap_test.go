package sap

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/habitat-network/habitat/internal/sync"

	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/events"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/stretchr/testify/require"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSap(t *testing.T) {
	// Configure default transport to skip TLS verification for the test servers
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	// 1. Setup Pear instance
	pearServer, orgId, adminId, spacesStore, orgHive := setupPear(t)
	t.Cleanup(func() {
		pearServer.CloseClientConnections()
		pearServer.Close()
	})

	createdURIs := make(map[string]bool)

	// 2. Initialize Pear with backfill data before starting SAP
	// We'll create 3 spaces and put 5 records in each (total 15 records)
	for i := range 3 {
		skey := fmt.Sprintf("backfill-space-%d", i)
		groupType := syntax.NSID("network.habitat.group")
		uri, err := spacesStore.CreateSpace(
			t.Context(),
			orgId.DID,
			adminId.DID,
			groupType,
			habitat_syntax.SpaceKey(skey),
		)
		require.NoError(t, err)

		collection := syntax.NSID("network.habitat.test")
		for j := range 5 {
			rkey := syntax.RecordKey(fmt.Sprintf("rkey-%d", j))
			recURI, _, err := spacesStore.PutRecord(
				t.Context(),
				uri,
				adminId.DID,
				collection,
				rkey,
				map[string]any{"data": fmt.Sprintf("backfill-%d-%d", i, j)},
			)
			require.NoError(t, err)
			createdURIs[recURI.String()] = true
		}
	}

	// 3. Setup test SAP instance
	mux := http.NewServeMux()
	sapServer := httptest.NewTLSServer(mux)
	t.Cleanup(sapServer.Close)

	db, err := gorm.Open(
		sqlite.Open(
			t.TempDir()+"/sap.db?_journal_mode=WAL",
		),
		&gorm.Config{},
	)
	require.NoError(t, err)

	store, err := oauthclient.NewGormStore(db)
	require.NoError(t, err)

	cfg := oauth.NewPublicConfig(
		sapServer.URL+"/client-metadata.json",
		sapServer.URL+"/oauth-callback",
		[]string{},
	)
	oauthApp := oauthclient.NewApp(&cfg, store, oauthclient.WithDirectory(orgHive))

	s, err := NewSap(SapConfig{
		OAuthClient: oauthApp,
		DB:          db,
		Directory:   orgHive,
	})
	require.NoError(t, err)

	mux.HandleFunc("/oauth-callback", func(w http.ResponseWriter, r *http.Request) {
		sess, err := oauthApp.ProcessCallback(t.Context(), r.URL.Query())
		require.NoError(t, err)

		require.NoError(t, s.AddManagedOrg(t.Context(), sess.AccountDID, sess.SessionID))
	})
	mux.HandleFunc("/client-metadata.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(oauthApp.Config.ClientMetadata()))
	})

	// 4. Start SAP sync worker loops
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(func() {
		cancel()
		pearServer.CloseClientConnections()
		pearServer.Close()
	})
	go func() {
		if err := s.Start(ctx); err != nil {
			t.Logf("Start returned: %v", err)
		}
	}()

	// 5. Complete OAuth authentication flow to add the org and trigger crawl
	redirectURL, err := oauthApp.StartAuthFlow(t.Context(), orgId.Handle.String())
	require.NoError(t, err)

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	client := &http.Client{
		Jar: jar,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	resp, err := client.Get(redirectURL)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected 200 but got %d. Body: %s", resp.StatusCode, string(body))
	}

	// 6. Create data to be synced during live subscription
	for i := range 2 {
		skey := fmt.Sprintf("live-space-%d", i)
		groupType := syntax.NSID("network.habitat.group")
		uri, err := spacesStore.CreateSpace(
			t.Context(),
			orgId.DID,
			adminId.DID,
			groupType,
			habitat_syntax.SpaceKey(skey),
		)
		require.NoError(t, err)

		collection := syntax.NSID("network.habitat.test")
		for j := range 5 {
			rkey := syntax.RecordKey(fmt.Sprintf("rkey-%d", j))
			recURI, _, err := spacesStore.PutRecord(
				t.Context(),
				uri,
				adminId.DID,
				collection,
				rkey,
				map[string]any{"data": fmt.Sprintf("live-%d-%d", i, j)},
			)
			require.NoError(t, err)
			createdURIs[recURI.String()] = true
		}
	}
	// Add 5 more records to backfill-space-0 (total 5 records)
	backfillGroupType := syntax.NSID("network.habitat.group")
	backfillSpaceURI := habitat_syntax.ConstructSpaceURI(
		orgId.DID,
		backfillGroupType,
		"backfill-space-0",
	)
	collection := syntax.NSID("network.habitat.test")
	for j := 5; j < 10; j++ {
		rkey := syntax.RecordKey(fmt.Sprintf("rkey-%d", j))
		recURI, _, err := spacesStore.PutRecord(
			t.Context(),
			backfillSpaceURI,
			adminId.DID,
			collection,
			rkey,
			map[string]any{"data": fmt.Sprintf("live-update-%d", j)},
		)
		require.NoError(t, err)
		createdURIs[recURI.String()] = true
	}

	// Total expected records = 15 (backfill) + 10 (new spaces) + 5 (existing space updates) = 30
	expectedCount := int64(len(createdURIs))
	require.Equal(t, int64(30), expectedCount)

	// 7. Validate that all records have been added to the outbox of SAP
	require.Eventually(t, func() bool {
		var count int64
		if err := db.Table("outboxes").Count(&count).Error; err != nil {
			return false
		}
		t.Logf("Current outbox count: %d/%d", count, expectedCount)
		return count == expectedCount
	}, 15*time.Second, 100*time.Millisecond)

	// Fetch and double check all matched URIs
	type localOutbox struct {
		ID    uint
		URI   string
		Value []byte
	}
	var outboxRecords []localOutbox
	err = db.Table("outboxes").Find(&outboxRecords).Error
	require.NoError(t, err)

	for _, rec := range outboxRecords {
		t.Logf("Outbox Record: ID=%d, URI=%s", rec.ID, rec.URI)
		require.Contains(t, createdURIs, rec.URI)
	}
}

func setupPear(
	t *testing.T,
) (*httptest.Server, *identity.Identity, *identity.Identity, spaces.Store, hive.Hive) {
	t.Helper()
	mux := http.NewServeMux()
	server := httptest.NewTLSServer(mux)

	fgaStore, err := fgastore.NewSQLite(t.Context(), t.TempDir()+"/pear.fga.db")
	require.NoError(t, err)
	db, err := gorm.Open(
		sqlite.Open(t.TempDir()+"/pear.db?_journal_mode=WAL"),
		&gorm.Config{},
	)
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	orgHive, err := hive.NewHive("hive.domain", strings.TrimPrefix(server.URL, "https://"), db)
	require.NoError(t, err)

	orgStore, err := org.NewStore(
		db,
		orgHive,
		orgHive,
		server.URL,
		nil,
		fgaStore,
	)
	require.NoError(t, err)

	oauthServer, err := oauthserver.NewOAuthServer(
		encrypt.TestKey,
		&org.LoginRouter{
			Pds: &passthroughProvider{redirectURI: server.URL + "/oauth-callback"},
		},
		orgHive,
		db,
		metricnoop.Meter{},
		orgStore,
		server.URL+"/oauth/token",
		oauthserver.NewJWTBearerStore(),
	)
	require.NoError(t, err)

	eventStore, err := events.NewStore(db)
	if err != nil {
		slog.ErrorContext(t.Context(), "unable to setup event store", "err", err)
		os.Exit(1)
	}

	syncServer := sync.NewServer(eventStore)

	spacesStore, err := spaces.NewStore(db, fgaStore, eventStore)
	if err != nil {
		slog.ErrorContext(t.Context(), "unable to setup spaces store", "err", err)
		os.Exit(1)
	}
	spacesServer := spaces.NewServer(
		spacesStore,
		fgaStore,
		oauthServer,
		authntest.NewFailMethod(),
		orgStore,
	)

	mux.HandleFunc("/xrpc/network.habitat.space.listSpaces", spacesServer.ListSpaces)
	mux.HandleFunc("/xrpc/network.habitat.sync.subscribeSpaces", syncServer.HandleSubscribeSpaces)
	mux.HandleFunc("/xrpc/network.habitat.space.getMembers", spacesServer.GetMembers)
	mux.HandleFunc("/xrpc/network.habitat.space.getRepoOplog", spacesServer.GetRepoOplog)
	mux.HandleFunc("/oauth/authorize", oauthServer.HandleAuthorize)
	mux.HandleFunc("/oauth/token", oauthServer.HandleToken)
	mux.HandleFunc("/oauth-callback", oauthServer.HandleCallback)

	orgId, adminId, err := orgStore.CreateOrg(
		t.Context(),
		"Test org",
		"admin",
		"",
		"atproto",
		"loginId",
		"org",
		"contact@example.com",
	)
	require.NoError(t, err)

	// Start sequencer after backfill data is created so that there's no
	// concurrent write contention on the Pear DB during backfill.
	go func() {
		require.ErrorIs(t, eventStore.StartSequencer(t.Context()), context.Canceled)
	}()

	return server, orgId, adminId, spacesStore, orgHive
}

type passthroughProvider struct {
	redirectURI string
}

// Authorize implements [login.Provider].
func (p *passthroughProvider) Authorize(
	ctx context.Context,
	loginHint string,
) (redirectURI string, state []byte, err error) {
	return p.redirectURI, nil, nil
}

// Exchange implements [login.Provider].
func (p *passthroughProvider) Exchange(
	ctx context.Context,
	code string,
	issuer string,
	state []byte,
) (loginId string, err error) {
	return "loginId", nil
}
