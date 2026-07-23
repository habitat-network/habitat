package sap

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	authn_testutil "github.com/habitat-network/habitat/internal/authn/testutil"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/events"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/notify"
	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// TestSap runs the full pear + sap loop: a session is added, sap backfills
// everything it can see and registers each discovered space for notifications;
// subsequent host writes are pushed by the host's notifier to sap's endpoint
// and drive incremental syncs — nothing is relayed manually — all while writes
// race the crawl. Every record must land in the outbox exactly once, with each
// repo's hash verified against the host's signed commit.
func TestSap(t *testing.T) {
	// Configure default transport to skip TLS verification for the test
	// servers (both sap's client calls and the host notifier's deliveries).
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	// 1. Setup the Pear host and its author identity.
	pear := setupPear(t)
	t.Cleanup(func() {
		pear.server.CloseClientConnections()
		pear.server.Close()
	})
	author := pear.author.DID

	createdURIs := make(map[string]bool)
	groupType := syntax.NSID("network.habitat.group")
	collection := syntax.NSID("network.habitat.test")

	createSpace := func(skey string) habitat_syntax.SpaceURI {
		uri, err := pear.store.CreateSpace(
			t.Context(), author, author, groupType, habitat_syntax.SpaceKey(skey))
		require.NoError(t, err)
		return uri
	}
	putRecord := func(space habitat_syntax.SpaceURI, rkey string, data string) {
		recURI, _, err := pear.store.PutRecord(
			t.Context(), space, author, collection,
			syntax.RecordKey(rkey), map[string]any{"data": data})
		require.NoError(t, err)
		createdURIs[recURI.String()] = true
	}

	// 2. Backfill data that exists before sap ever connects:
	// 3 spaces with 5 records each (15 records).
	for i := range 3 {
		uri := createSpace(fmt.Sprintf("backfill-space-%d", i))
		for j := range 5 {
			putRecord(uri, fmt.Sprintf("rkey-%d", j), fmt.Sprintf("backfill-%d-%d", i, j))
		}
	}

	// 3. Setup the sap instance with its public notify endpoint.
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
	oauthApp := oauth.NewClientApp(&cfg, store)

	s, err := New(Config{
		DB:          db,
		OAuthClient: oauthApp,
		Directory:   pear.hive,
		Endpoint:    sapServer.URL,
		// Fast re-crawls so spaces created mid-test are discovered promptly.
		CrawlInterval: 200 * time.Millisecond,
	})
	require.NoError(t, err)

	// The notify entry points, as cmd/sap mounts them (sans service auth).
	mux.HandleFunc("/xrpc/network.habitat.space.notifyWrite",
		func(w http.ResponseWriter, r *http.Request) {
			var input struct {
				Space string `json:"space"`
				Repo  string `json:"repo"`
				Rev   string `json:"rev"`
				Hash  string `json:"hash"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			var hash []byte
			if input.Hash != "" {
				hash, err = base64.StdEncoding.DecodeString(input.Hash)
				require.NoError(t, err)
			}
			require.NoError(t, s.NotifyWrite(
				r.Context(),
				habitat_syntax.SpaceURI(input.Space),
				syntax.DID(input.Repo),
				syntax.TID(input.Rev),
				hash,
			))
			w.WriteHeader(http.StatusOK)
		})
	mux.HandleFunc("/xrpc/network.habitat.space.notifySpaceDeleted",
		func(w http.ResponseWriter, r *http.Request) {
			var input struct {
				Space string `json:"space"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			require.NoError(t, s.NotifySpaceDeleted(
				r.Context(), habitat_syntax.SpaceURI(input.Space)))
			w.WriteHeader(http.StatusOK)
		})

	// 4. Start sap and add the session directly (the caller completed the
	// OAuth flow; sap only needs the session data).
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go func() {
		require.NoError(t, s.Start(ctx))
	}()

	require.NoError(t, store.SaveSession(t.Context(), oauth.ClientSessionData{
		AccountDID:              author,
		SessionID:               "sess1",
		HostURL:                 pear.server.URL,
		AccessToken:             futureJWT(t),
		DPoPPrivateKeyMultibase: testDPoPKey(t),
	}))
	require.NoError(t, s.AddSession(t.Context(), author, "sess1"))

	// 5. The crawl registers every discovered space for notifications.
	require.Eventually(t, func() bool {
		var count int64
		if err := db.Table("sap_registrations").Count(&count).Error; err != nil {
			return false
		}
		return count >= 3
	}, 10*time.Second, 50*time.Millisecond, "backfill spaces never registered")

	// 6. Live writes, racing the crawl and re-crawls. Nothing is relayed by
	// hand: writes to registered spaces arrive via the host's notifyWrite
	// push; new spaces are found by the periodic re-crawl, registered, and
	// synced.
	for i := range 2 {
		uri := createSpace(fmt.Sprintf("live-space-%d", i))
		for j := range 5 {
			putRecord(uri, fmt.Sprintf("rkey-%d", j), fmt.Sprintf("live-%d-%d", i, j))
		}
	}
	backfillSpace := habitat_syntax.ConstructSpaceURI(author, groupType, "backfill-space-0")
	for j := 5; j < 10; j++ {
		putRecord(backfillSpace, fmt.Sprintf("rkey-%d", j), fmt.Sprintf("live-update-%d", j))
	}

	// Total = 15 (backfill) + 10 (new spaces) + 5 (updates to a crawled space).
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
		URI string
	}
	var outboxRecords []outboxRow
	require.NoError(t, db.Table("sap_outbox").Find(&outboxRecords).Error)
	for _, rec := range outboxRecords {
		require.Contains(t, createdURIs, rec.URI)
	}

	// 8. All 5 spaces are registered for notifications, and every tracked repo
	// settled active with a verified hash.
	var regCount int64
	require.NoError(t, db.Table("sap_registrations").Count(&regCount).Error)
	require.Equal(t, int64(5), regCount)

	type repoRow struct {
		Space string
		State string
		Hash  []byte
	}
	var repos []repoRow
	require.NoError(t, db.Table("sap_repos").Find(&repos).Error)
	require.Len(t, repos, 5)
	for _, r := range repos {
		require.Equal(t, "active", r.State, "repo in space %s not active", r.Space)
		require.NotEmpty(t, r.Hash)
	}
}

// pearHost bundles the host-side pieces the test drives.
type pearHost struct {
	server *httptest.Server
	store  spaces.Store
	hive   hive.Hive
	author *identity.Identity
}

// setupPear wires a minimal pear host: a spaces store whose notifier really
// delivers to registered endpoints, the read/sync XRPC surface, and
// registerNotify. Auth is a stub that authenticates every request as the
// author under the EveryoneOrg — no org store or OAuth server involved.
func setupPear(t *testing.T) *pearHost {
	t.Helper()
	mux := http.NewServeMux()
	server := httptest.NewTLSServer(mux)

	db := db_testutil.NewDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	fgaStore, err := fgastore.NewSQLite(t.Context(), t.TempDir()+"/pear.fga.db")
	require.NoError(t, err)

	orgHive, err := hive.NewHive("hive.domain", strings.TrimPrefix(server.URL, "https://"), db)
	require.NoError(t, err)
	author, err := orgHive.MintOrgIdentity(t.Context(), "author")
	require.NoError(t, err)

	eventStore, err := events.NewStore(db)
	require.NoError(t, err)
	notifyStore, err := notify.NewStore(db)
	require.NoError(t, err)
	notifier := notify.NewNotifier(notifyStore, http.DefaultClient, orgHive)

	spacesStore, err := spaces.NewStore(db, fgaStore, eventStore, notifier)
	require.NoError(t, err)

	auth := authn_testutil.NewSuccessMethodForOrg(author.DID, &org.EveryoneOrg{})
	spacesServer := spaces.NewServer(
		spacesStore,
		fgaStore,
		auth,
		authn_testutil.NewFailMethod(),
		nil, // delegation: getSpaceCredential is not mounted
		nil, // org store: only used by the CreateSpace handler, not mounted
		nil, // host key: managed authors sign with their own hive keys
		orgHive,
	)
	notifyServer := notify.NewServer(notifyStore, spacesStore, auth)

	mux.HandleFunc("/xrpc/network.habitat.space.listSpaces", spacesServer.ListSpaces)
	mux.HandleFunc("/xrpc/network.habitat.space.listRepos", spacesServer.ListRepos)
	mux.HandleFunc("/xrpc/network.habitat.space.listRepoOps", spacesServer.ListRepoOps)
	mux.HandleFunc("/xrpc/com.atproto.space.getRepo", spacesServer.GetRepo)
	mux.HandleFunc("/xrpc/network.habitat.space.registerNotify", notifyServer.RegisterNotify)

	go func() {
		require.ErrorIs(t, eventStore.StartSequencer(t.Context()), context.Canceled)
	}()

	return &pearHost{server: server, store: spacesStore, hive: orgHive, author: author}
}

// testDPoPKey returns a valid DPoP private key multibase so ResumeSession can
// parse the fabricated session data.
func testDPoPKey(t *testing.T) string {
	t.Helper()
	key, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	return key.Multibase()
}

// futureJWT returns a JWT whose only meaningful claim is an expiry far in the
// future, so the OAuth transport treats the access token as valid and never
// attempts a refresh; the host's stub auth accepts anything.
func futureJWT(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tok, err := jwt.NewWithClaims(jwt.SigningMethodPS256,
		jwt.MapClaims{"exp": time.Now().Add(time.Hour).Unix(), "jti": "test"},
	).SignedString(key)
	require.NoError(t, err)
	return tok
}
