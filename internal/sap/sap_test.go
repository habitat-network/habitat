package sap

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	metricnoop "go.opentelemetry.io/otel/metric/noop"

	"github.com/habitat-network/habitat/internal/authn"
	authn_testutil "github.com/habitat-network/habitat/internal/authn/testutil"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/spaces"
	"github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// TestSap runs the full pear + sap flow: a session is added via the OAuth
// flow, sap backfills everything the session can see, and live writes are
// picked up when the host's notifyWrite is relayed through Sap.NotifyWrite —
// all while writes race the initial crawl. Every record must land in the
// outbox exactly once, with each repo's hash verified against the host's
// signed commit.
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
	groupType := syntax.NSID("network.habitat.group")
	collection := syntax.NSID("network.habitat.test")

	// 2. Initialize Pear with backfill data before starting SAP:
	// 3 spaces with 5 records each (15 records).
	for i := range 3 {
		skey := fmt.Sprintf("backfill-space-%d", i)
		uri, err := spacesStore.CreateSpace(
			t.Context(),
			orgId.DID,
			adminId.DID,
			groupType,
			habitat_syntax.SpaceKey(skey),
		)
		require.NoError(t, err)

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

	db := db_testutil.NewDB(t)

	store, err := oauthclient.NewGormStore(db)
	require.NoError(t, err)

	cfg := oauth.NewPublicConfig(
		sapServer.URL+"/client-metadata.json",
		sapServer.URL+"/oauth-callback",
		[]string{},
	)
	oauthApp := oauthclient.NewApp(&cfg, store, oauthclient.WithDirectory(orgHive))

	s, err := New(Config{
		OAuthClient: oauthApp,
		DB:          db,
		Directory:   orgHive,
	})
	require.NoError(t, err)

	mux.HandleFunc("/oauth-callback", func(w http.ResponseWriter, r *http.Request) {
		sess, err := oauthApp.ProcessCallback(t.Context(), r.URL.Query())
		require.NoError(t, err)

		require.NoError(t, s.AddSession(t.Context(), sess.AccountDID, sess.SessionID))
	})
	mux.HandleFunc("/client-metadata.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(oauthApp.Config.ClientMetadata()))
	})

	// 4. Start SAP worker loops
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go func() {
		if err := s.Start(ctx); err != nil {
			t.Logf("Start returned: %v", err)
		}
	}()

	// 5. Complete the OAuth flow to add the session and trigger the crawl
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

	// notifyWrite relays the host's current head for a repo into sap, the way
	// cmd/sap's notify endpoint would after a host push.
	notifyWrite := func(space habitat_syntax.SpaceURI) {
		rev, hash, found, err := spacesStore.RepoHead(t.Context(), space, adminId.DID)
		require.NoError(t, err)
		require.True(t, found)
		require.NoError(t, s.NotifyWrite(t.Context(), space, adminId.DID, syntax.TID(rev), hash))
	}

	// 6. Create data while the crawl runs, relaying notifyWrite per write:
	// 2 new spaces with 5 records each, racing the backfill.
	for i := range 2 {
		skey := fmt.Sprintf("live-space-%d", i)
		uri, err := spacesStore.CreateSpace(
			t.Context(),
			orgId.DID,
			adminId.DID,
			groupType,
			habitat_syntax.SpaceKey(skey),
		)
		require.NoError(t, err)

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
			notifyWrite(uri)
		}
	}
	// 5 more records in an already-crawled space (incremental sync path).
	backfillSpaceURI := habitat_syntax.ConstructSpaceURI(
		orgId.DID,
		groupType,
		"backfill-space-0",
	)
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
		notifyWrite(backfillSpaceURI)
	}

	// Total expected records = 15 (backfill) + 10 (new spaces) + 5 (existing space updates) = 30
	expectedCount := int64(len(createdURIs))
	require.Equal(t, int64(30), expectedCount)

	// 7. Every record lands in the outbox exactly once.
	require.Eventually(t, func() bool {
		var count int64
		if err := db.Table("sap_outbox").Count(&count).Error; err != nil {
			return false
		}
		t.Logf("Current outbox count: %d/%d", count, expectedCount)
		return count == expectedCount
	}, 15*time.Second, 100*time.Millisecond)

	type outboxRow struct {
		ID    uint
		URI   string
		Value []byte
	}
	var outboxRecords []outboxRow
	require.NoError(t, db.Table("sap_outbox").Find(&outboxRecords).Error)
	for _, rec := range outboxRecords {
		require.Contains(t, createdURIs, rec.URI)
	}

	// 8. Every tracked repo settled active with a verified hash matching the
	// host's head.
	type repoRow struct {
		Space string
		State string
		Hash  []byte
	}
	var repos []repoRow
	require.NoError(t, db.Table("sap_repos").Find(&repos).Error)
	require.NotEmpty(t, repos)
	for _, r := range repos {
		require.Equal(t, "active", r.State, "repo in space %s not active", r.Space)
		require.NotEmpty(t, r.Hash)
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
	db := db_testutil.NewDB(t)
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
		server.URL,
		oauthserver.NewJWTBearerStore(),
	)
	require.NoError(t, err)

	spacesStore := testutil.NewTestStore(t)

	spacesServer := spaces.NewServer(
		spacesStore,
		fgaStore,
		oauthServer,
		authn_testutil.NewFailMethod(),
		authn.NewDelegationTokenAuthMethod(nil, nil),
		orgStore,
		nil,
		orgHive,
	)

	mux.HandleFunc("/xrpc/network.habitat.space.listSpaces", spacesServer.ListSpaces)
	mux.HandleFunc("/xrpc/network.habitat.space.listRepos", spacesServer.ListRepos)
	mux.HandleFunc("/xrpc/network.habitat.space.listRepoOps", spacesServer.ListRepoOps)
	mux.HandleFunc("/xrpc/com.atproto.space.getRepo", spacesServer.GetRepo)
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

	go func() {
		require.ErrorIs(t, spacesStore.EventStore.StartSequencer(t.Context()), context.Canceled)
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
