package sap

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
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

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	authn_testutil "github.com/habitat-network/habitat/internal/authn/testutil"
	"github.com/habitat-network/habitat/internal/clientmeta"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/notify"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_server "github.com/habitat-network/habitat/internal/spaces/server"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/pkg/oauthclient"
	"github.com/habitat-network/habitat/pkg/sap/outbox"
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
			t.Context(), author, groupType, habitat_syntax.SpaceKey(skey),
		)
		require.NoError(t, err)
		return uri
	}
	putRecord := func(space habitat_syntax.SpaceURI, rkey string, data string) {
		recURI, _, err := pear.store.PutRecord(
			t.Context(),
			space,
			author,
			collection,
			syntax.RecordKey(
				rkey,
			),
			spaces_testutil.MustMarshalRecord(t, map[string]any{"data": data}),
		)
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
			var input habitat.NetworkHabitatSpaceNotifyWriteInput
			require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			require.NoError(t, s.NotifyWrite(
				r.Context(),
				habitat_syntax.SpaceURI(input.Space).URI(),
				syntax.DID(input.Repo),
				syntax.TID(input.Rev),
				input.Hash,
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
				r.Context(), habitat_syntax.SpaceURI(input.Space).URI(),
			))
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
		if err := db.Table("registrations").Count(&count).Error; err != nil {
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

	// 7. Every record lands in the outbox exactly once, each with the URI of
	// a record actually created above.
	var msgs []outbox.Message
	require.Eventually(t, func() bool {
		var err error
		msgs, err = s.Outbox().Poll(t.Context(), int(expectedCount)+1)
		require.NoError(t, err)
		t.Logf("Current outbox count: %d/%d", len(msgs), expectedCount)
		return int64(len(msgs)) == expectedCount
	}, 15*time.Second, 100*time.Millisecond)
	for _, msg := range msgs {
		require.Contains(t, createdURIs, string(msg.URI))
	}

	// 8. All 5 spaces are registered for notifications, and every tracked repo
	// settled active with a verified hash. A repo can still bounce back to
	// pending/syncing after its record is outboxed: a racing recrawl Check
	// landing mid-flight marks it dirty unconditionally (engine.observeHead),
	// forcing one more (usually no-op) pass before it resettles — so this
	// polls rather than asserting once.
	var regCount int64
	require.NoError(t, db.Table("registrations").Count(&regCount).Error)
	require.Equal(t, int64(5), regCount)

	type repoRow struct {
		Space string
		State string
		Hash  []byte
	}
	var repos []repoRow
	require.Eventually(t, func() bool {
		if err := db.Table("repos").Find(&repos).Error; err != nil {
			return false
		}
		if len(repos) != 5 {
			return false
		}
		for _, r := range repos {
			if r.State != "active" {
				return false
			}
		}
		return true
	}, 5*time.Second, 50*time.Millisecond, "not all repos settled active")
	for _, r := range repos {
		require.NotEmpty(t, r.Hash)
	}

	// 9. Sessions reports the DID sap is syncing on behalf of.
	dids, err := s.Sessions(t.Context())
	require.NoError(t, err)
	require.Equal(t, []syntax.DID{author}, dids)

	// 10. Once every message is acked, none are redelivered.
	for _, msg := range msgs {
		require.NoError(t, s.Outbox().Ack(t.Context(), msg.ID))
	}
	remaining, err := s.Outbox().Poll(t.Context(), 100)
	require.NoError(t, err)
	require.Empty(t, remaining)

	// 11. NotifySpaceDeleted drops all local tracking state for a space: its
	// registration and repo row disappear, and the space is no longer synced.
	require.NoError(t, s.NotifySpaceDeleted(t.Context(), backfillSpace.URI()))

	require.NoError(t, db.Table("registrations").Count(&regCount).Error)
	require.Equal(t, int64(4), regCount)

	require.NoError(t, db.Table("repos").Find(&repos).Error)
	require.Len(t, repos, 4)
	for _, r := range repos {
		require.NotEqual(t, backfillSpace.String(), r.Space)
	}
}

// TestSapTrackSpace verifies that TrackSpace tracks a space the same way the
// crawl would if its listSpaces discovered it — recording space access,
// registering for notifications, and syncing the space's repos — without the
// session ever being crawled. The session is never added, so only TrackSpace
// can discover the space.
func TestSapTrackSpace(t *testing.T) {
	// Configure default transport to skip TLS verification for the test
	// servers (sap's credential exchange and repo-host reads).
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	pear := setupPear(t)
	t.Cleanup(func() {
		pear.server.CloseClientConnections()
		pear.server.Close()
	})
	author := pear.author.DID

	groupType := syntax.NSID("network.habitat.group")
	collection := syntax.NSID("network.habitat.test")
	space, err := pear.store.CreateSpace(
		t.Context(), author, groupType, habitat_syntax.SpaceKey("tracked-space"),
	)
	require.NoError(t, err)
	recURI, _, err := pear.store.PutRecord(
		t.Context(),
		space,
		author,
		collection,
		syntax.RecordKey(
			"rkey-0",
		),
		spaces_testutil.MustMarshalRecord(t, map[string]any{"data": "tracked"}),
	)
	require.NoError(t, err)

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
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go func() {
		require.NoError(t, s.Start(ctx))
	}()

	// The session is resumable for TrackSpace's space-credential delegation,
	// but is never added, so no crawl discovers the space.
	require.NoError(t, store.SaveSession(t.Context(), oauth.ClientSessionData{
		AccountDID:              author,
		SessionID:               "sess1",
		HostURL:                 pear.server.URL,
		AccessToken:             futureJWT(t),
		DPoPPrivateKeyMultibase: testDPoPKey(t),
	}))

	require.NoError(t, s.TrackSpace(t.Context(), space.URI(), author, "sess1"))

	// The space's record lands in the outbox, as it would after a crawl.
	var got []outbox.Message
	require.Eventually(t, func() bool {
		var err error
		got, err = s.Outbox().Poll(t.Context(), 10)
		require.NoError(t, err)
		return len(got) >= 1
	}, 15*time.Second, 100*time.Millisecond)
	for _, msg := range got {
		require.Equal(t, recURI.String(), string(msg.URI))
	}

	// The space is registered for notifications and its repo is tracked.
	var regCount int64
	require.NoError(t, db.Table("registrations").Count(&regCount).Error)
	require.Equal(t, int64(1), regCount)

	var repoCount int64
	require.NoError(t, db.Table("repos").Count(&repoCount).Error)
	require.Equal(t, int64(1), repoCount)
}

// TestSapRecrawl verifies that Recrawl re-crawls a session even when a prior
// crawl for it is stuck errored with a stale cursor — the scenario Recrawl
// exists for, distinguishing it from a crawl.Crawler.Run resume.
func TestSapRecrawl(t *testing.T) {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	pear := setupPear(t)
	t.Cleanup(func() {
		pear.server.CloseClientConnections()
		pear.server.Close()
	})
	author := pear.author.DID

	groupType := syntax.NSID("network.habitat.group")
	collection := syntax.NSID("network.habitat.test")
	space, err := pear.store.CreateSpace(
		t.Context(), author, groupType, habitat_syntax.SpaceKey("recrawl-space"),
	)
	require.NoError(t, err)
	recURI, _, err := pear.store.PutRecord(
		t.Context(),
		space,
		author,
		collection,
		syntax.RecordKey(
			"rkey-0",
		),
		spaces_testutil.MustMarshalRecord(t, map[string]any{"data": "recrawled"}),
	)
	require.NoError(t, err)

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
	})
	require.NoError(t, err)

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

	// Seed a crawl left stuck errored with a stale cursor, as if a previous
	// crawl for this session failed partway through.
	require.NoError(t, db.Exec(
		`INSERT INTO crawls (did, session_id, state, cursor, error_msg, created_at, updated_at)
		 VALUES (?, 'sess1', 'errored', 'stale-cursor', 'boom', ?, ?)`,
		author, time.Now(), time.Now(),
	).Error)

	s.Recrawl(t.Context(), author, "sess1")

	var got []outbox.Message
	require.Eventually(t, func() bool {
		var err error
		got, err = s.Outbox().Poll(t.Context(), 10)
		require.NoError(t, err)
		return len(got) >= 1
	}, 15*time.Second, 100*time.Millisecond)
	for _, msg := range got {
		require.Equal(t, recURI.String(), string(msg.URI))
	}

	type crawlRow struct {
		State  string
		Cursor string
	}
	var cr crawlRow
	require.Eventually(t, func() bool {
		if err := db.Table("crawls").Where("did = ?", author).First(&cr).Error; err != nil {
			return false
		}
		return cr.State == "complete"
	}, 5*time.Second, 50*time.Millisecond, "crawl never completed")
	require.Empty(t, cr.Cursor)
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

	orgHive, err := hive.NewHive("hive.domain", strings.TrimPrefix(server.URL, "https://"), db)
	require.NoError(t, err)
	author, err := orgHive.MintOrgIdentity(t.Context(), "author")
	require.NoError(t, err)

	notifyStore, err := notify.NewStore(db)
	require.NoError(t, err)

	// Managed authors sign with their own hive keys.
	spacesStore := spaces_testutil.NewTestStore(t, spaces_testutil.WithMemberSigner(orgHive))

	require.NoError(t, err)

	everyone := org.NewEveryoneOrg(strings.TrimPrefix(server.URL, "https://"))
	validator := authn_testutil.NewSuccessValidator(
		&authn.CredentialInfo{Subject: author.DID, Org: everyone},
	)
	spacesServer := spaces_server.NewServer(
		spacesStore,
		validator,
		nil, // host key: managed authors sign with their own hive keys
		orgHive,
		nil, // blobs: no blob handlers mounted
		clientmeta.NewResolver(),
	)
	notifyServer := notify.NewServer(notifyStore, validator)

	mux.HandleFunc("/xrpc/network.habitat.space.listSpaces", spacesServer.ListSpaces)
	mux.HandleFunc("/xrpc/network.habitat.space.listRepos", spacesServer.ListRepos)
	mux.HandleFunc("/xrpc/network.habitat.space.listRepoOps", spacesServer.ListRepoOps)
	mux.HandleFunc("/xrpc/network.habitat.space.getRepo", spacesServer.GetRepo)
	mux.HandleFunc("/xrpc/network.habitat.space.registerNotify", notifyServer.RegisterNotify)
	mux.HandleFunc("/xrpc/network.habitat.space.getDelegationToken",
		func(w http.ResponseWriter, r *http.Request) {
			httpx.WriteJSON(r.Context(), w,
				habitat.NetworkHabitatSpaceGetDelegationTokenOutput{Token: "test-delegation"})
		})
	mux.HandleFunc("/xrpc/network.habitat.space.getSpaceCredential",
		func(w http.ResponseWriter, r *http.Request) {
			httpx.WriteJSON(r.Context(), w,
				habitat.NetworkHabitatSpaceGetSpaceCredentialOutput{Credential: "test-credential"})
		})

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
	tok, err := jwt.NewWithClaims(
		jwt.SigningMethodPS256,
		jwt.MapClaims{"exp": time.Now().Add(time.Hour).Unix(), "jti": "test"},
	).SignedString(key)
	require.NoError(t, err)
	return tok
}
