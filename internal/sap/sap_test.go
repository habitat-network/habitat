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
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	metricnoop "go.opentelemetry.io/otel/metric/noop"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	authn_testutil "github.com/habitat-network/habitat/internal/authn/testutil"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/hive"
	login_testutil "github.com/habitat-network/habitat/internal/login/testutil"
	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/spaces"
	space_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
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
	pearServer, spacesStore, orgHive := setupPear(t)
	t.Cleanup(func() {
		pearServer.CloseClientConnections()
		pearServer.Close()
	})

	createdURIs := make(map[string]bool)
	groupType := syntax.NSID("network.habitat.group")
	collection := syntax.NSID("network.habitat.test")

	org := syntax.DID("did:web:public.habitat.network")
	user := syntax.DID("did:web:user.habitat.network")

	// 2. Initialize Pear with backfill data before starting SAP:
	// 3 spaces with 5 records each (15 records).
	for i := range 3 {
		skey := fmt.Sprintf("backfill-space-%d", i)
		uri, err := spacesStore.CreateSpace(
			t.Context(),
			org,
			user,
			groupType,
			habitat_syntax.SpaceKey(skey),
		)
		require.NoError(t, err)

		for j := range 5 {
			rkey := syntax.RecordKey(fmt.Sprintf("rkey-%d", j))
			recURI, _, err := spacesStore.PutRecord(
				t.Context(),
				uri,
				user,
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
	mux.HandleFunc(
		"/xrpc/network.habitat.space.notifyWrite",
		func(w http.ResponseWriter, r *http.Request) {
			var input habitat.NetworkHabitatSpaceNotifyWriteInput
			require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			require.NoError(
				t,
				s.NotifyWrite(
					t.Context(),
					habitat_syntax.SpaceURI(input.Space),
					syntax.DID(input.Repo),
					syntax.TID(input.Rev),
					input.Hash.([]byte),
				),
			)
		},
	)

	// 4. Start SAP worker loops
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go func() {
		require.NoError(t, s.Start(ctx))
	}()

	// 5. Complete the OAuth flow to add the session and trigger the crawl
	redirectURL, err := oauthApp.StartAuthFlow(t.Context(), user.String())
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

	// 6. Create data while the crawl runs
	// 2 new spaces with 5 records each, racing the backfill.
	for i := range 2 {
		skey := fmt.Sprintf("live-space-%d", i)
		uri, err := spacesStore.CreateSpace(
			t.Context(),
			org,
			user,
			groupType,
			habitat_syntax.SpaceKey(skey),
		)
		require.NoError(t, err)

		for j := range 5 {
			rkey := syntax.RecordKey(fmt.Sprintf("rkey-%d", j))
			recURI, _, err := spacesStore.PutRecord(
				t.Context(),
				uri,
				user,
				collection,
				rkey,
				map[string]any{"data": fmt.Sprintf("live-%d-%d", i, j)},
			)
			require.NoError(t, err)
			createdURIs[recURI.String()] = true
		}
	}
	// 5 more records in an already-crawled space (incremental sync path).
	backfillSpaceURI := habitat_syntax.ConstructSpaceURI(
		syntax.DID("did:web:public.habitat.network"),
		groupType,
		"backfill-space-0",
	)
	for j := 5; j < 10; j++ {
		rkey := syntax.RecordKey(fmt.Sprintf("rkey-%d", j))
		recURI, _, err := spacesStore.PutRecord(
			t.Context(),
			backfillSpaceURI,
			user,
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

	// 7. Every record lands in the outbox exactly once.
	require.Eventually(t, func() bool {
		var count int64
		if err := db.Table("sap_outbox").Count(&count).Error; err != nil {
			return false
		}
		t.Logf("Current outbox count: %d/%d", count, expectedCount)
		return count == expectedCount
	}, 15*time.Second, 100*time.Millisecond)

	messages, err := s.Outbox().Poll(t.Context(), 30)
	require.NoError(t, err)
	require.Len(t, messages, int(expectedCount))
}

func setupPear(
	t *testing.T,
) (*httptest.Server, spaces.Store, hive.Hive) {
	t.Helper()
	mux := http.NewServeMux()
	server := httptest.NewTLSServer(mux)

	fgaStore, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	db := db_testutil.NewDB(t)

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

	pds := login_testutil.NewPassthroughProvider(t)
	pds.RedirectURI = server.URL + "/oauth-callback"
	oauthServer, err := oauthserver.NewOAuthServer(
		encrypt.TestKey,
		&org.LoginRouter{Pds: pds},
		orgHive,
		db,
		metricnoop.Meter{},
		orgStore,
		server.URL,
		oauthserver.NewJWTBearerStore(),
	)
	require.NoError(t, err)

	spacesStore := space_testutil.NewTestStore(t)

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

	return server, spacesStore, orgHive
}
