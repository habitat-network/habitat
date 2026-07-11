package sap

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/habitat-network/habitat/internal/spacecommit"
	"github.com/habitat-network/habitat/internal/spaces"
	"github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestResyncer_SyncRepo(t *testing.T) {
	t.Parallel()

	spaceURI := "ats://did:plc:testorg/network.habitat.space/my-space"
	clock := syntax.NewTIDClock(0)
	rev1 := clock.Next().String()
	rev2 := clock.Next().String()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/xrpc/network.habitat.space.listRepoOps", r.URL.Path)
		since := r.URL.Query().Get("since")
		switch since {
		case "":
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListRepoOpsOutput{
				Ops: []habitat.NetworkHabitatSpaceListRepoOpsOpEntry{
					{
						Rev:        rev1,
						Collection: "network.habitat.note",
						Rkey:       "k1",
						Value:      map[string]any{"text": "hello"},
					},
					{
						Rev:        rev2,
						Collection: "network.habitat.note",
						Rkey:       "k2",
						Value:      map[string]any{"text": "world"},
					},
				},
				Cursor: rev2,
			})
		default:
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListRepoOpsOutput{
				Ops: []habitat.NetworkHabitatSpaceListRepoOpsOpEntry{},
			})
		}
	}))
	t.Cleanup(srv.Close)

	db := openTestDB(t)
	resyncNotif := utils.NewPollNotifier()
	outboxNotif := utils.NewPollNotifier()
	store, err := oauthclient.NewGormStore(db)
	require.NoError(t, err)
	cfg := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)
	oauthApp := oauthclient.NewApp(&cfg, store)
	resyncBuf := newResyncBuffer(db, resyncNotif, outboxNotif)
	resyncer := newResyncer(db, oauthApp, resyncBuf, resyncNotif, outboxNotif, 1, newTestMetrics(t))

	tk := testJWT(t)
	require.NoError(t, store.SaveSession(t.Context(), oauth.ClientSessionData{
		AccountDID:  "did:plc:testorg",
		SessionID:   "sess1",
		HostURL:     srv.URL,
		AccessToken: tk,
	}))
	require.NoError(t, db.Create(&managedOrg{
		DID:       "did:plc:testorg",
		SessionID: "sess1",
	}).Error)

	space := habitat_syntax.SpaceURI(spaceURI)
	repoDID := syntax.DID("did:plc:member1")
	require.NoError(t, db.Clauses(clause.OnConflict{DoNothing: true}).
		Create(&managedRepo{Space: space, DID: repoDID, State: RepoStatePending}).Error)
	require.NoError(t, db.Model(&managedRepo{}).
		Where("space = ? AND did = ?", space, repoDID).
		Update("state", RepoStateResyncing).Error)

	require.NoError(t, resyncer.syncRepo(t.Context(), space, repoDID))

	var records []outbox
	require.NoError(t, db.Find(&records).Error)
	require.Len(t, records, 2)

	var repo managedRepo
	require.NoError(t, db.Where("space = ? AND did = ?", space, repoDID).First(&repo).Error)
	require.Equal(t, RepoStateActive, repo.State)
	require.Equal(t, syntax.TID(rev2), repo.Rev)
}

// setupResyncerFor wires a resyncer with an oauth session for did:plc:testorg
// pointing at srvURL, plus a matching managedOrg. It returns the resyncer and db.
func setupResyncerFor(t *testing.T, srvURL string) (*resyncer, *gorm.DB) {
	t.Helper()
	db := openTestDB(t)
	resyncNotif := utils.NewPollNotifier()
	outboxNotif := utils.NewPollNotifier()
	store, err := oauthclient.NewGormStore(db)
	require.NoError(t, err)
	cfg := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)
	oauthApp := oauthclient.NewApp(&cfg, store)
	resyncBuf := newResyncBuffer(db, resyncNotif, outboxNotif)
	resyncer := newResyncer(db, oauthApp, resyncBuf, resyncNotif, outboxNotif, 1, newTestMetrics(t))

	require.NoError(t, store.SaveSession(t.Context(), oauth.ClientSessionData{
		AccountDID:  "did:plc:testorg",
		SessionID:   "sess1",
		HostURL:     srvURL,
		AccessToken: testJWT(t),
	}))
	require.NoError(t, db.Create(&managedOrg{DID: "did:plc:testorg", SessionID: "sess1"}).Error)
	return resyncer, db
}

// TestResyncer_VerifiesHeadCommit checks that a full pull whose folded LtHash
// matches the signed commit at head reaches active and persists the hash state.
func TestResyncer_VerifiesHeadCommit(t *testing.T) {
	t.Parallel()

	spaceURI := "ats://did:plc:testorg/network.habitat.space/my-space"
	clock := syntax.NewTIDClock(0)
	rev1 := clock.Next().String()
	rev2 := clock.Next().String()

	ops := []habitat.NetworkHabitatSpaceListRepoOpsOpEntry{
		{Rev: rev1, Collection: "network.habitat.note", Rkey: "k1", Cid: "bafyreiaaa", Value: map[string]any{"text": "hello"}},
		{Rev: rev2, Collection: "network.habitat.note", Rkey: "k2", Cid: "bafyreibbb", Value: map[string]any{"text": "world"}},
	}
	var lt spacecommit.LtHash
	require.NoError(t, foldOps(&lt, ops))
	commit := habitat.NetworkHabitatSpaceDefsSignedCommit{
		Ver:  int64(spacecommit.Version),
		Rev:  rev2,
		Hash: base64.StdEncoding.EncodeToString(lt.Sum()),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/xrpc/network.habitat.space.listRepoOps", r.URL.Path)
		if r.URL.Query().Get("since") == "" {
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListRepoOpsOutput{Ops: ops, Cursor: rev2})
			return
		}
		// Head of the oplog: no further ops, signed commit included.
		_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListRepoOpsOutput{
			Ops:    []habitat.NetworkHabitatSpaceListRepoOpsOpEntry{},
			Commit: commit,
		})
	}))
	t.Cleanup(srv.Close)

	resyncer, db := setupResyncerFor(t, srv.URL)
	space := habitat_syntax.SpaceURI(spaceURI)
	repoDID := syntax.DID("did:plc:member1")
	require.NoError(t, db.Create(&managedRepo{Space: space, DID: repoDID, State: RepoStateResyncing}).Error)

	require.NoError(t, resyncer.syncRepo(t.Context(), space, repoDID))

	var repo managedRepo
	require.NoError(t, db.Where("space = ? AND did = ?", space, repoDID).First(&repo).Error)
	require.Equal(t, RepoStateActive, repo.State)
	require.Equal(t, syntax.TID(rev2), repo.Rev)
	require.Equal(t, lt.State(), repo.Hash)
}

// TestResyncer_HashMismatchMarksDesynced checks that a full pull whose folded
// LtHash disagrees with the signed commit is rejected: the repo is reset to
// desynced (rev and hash cleared) for a fresh full pull rather than going active.
func TestResyncer_HashMismatchMarksDesynced(t *testing.T) {
	t.Parallel()

	spaceURI := "ats://did:plc:testorg/network.habitat.space/my-space"
	clock := syntax.NewTIDClock(0)
	rev1 := clock.Next().String()

	ops := []habitat.NetworkHabitatSpaceListRepoOpsOpEntry{
		{Rev: rev1, Collection: "network.habitat.note", Rkey: "k1", Cid: "bafyreiaaa", Value: map[string]any{"text": "hello"}},
	}
	// A commit hash that does not match the folded ops (a dropped/mutated op).
	var wrong spacecommit.LtHash
	wrong.Add(spacecommit.RecordElement("network.habitat.note", "k1", "bafyreidifferent"))
	commit := habitat.NetworkHabitatSpaceDefsSignedCommit{
		Ver:  int64(spacecommit.Version),
		Rev:  rev1,
		Hash: base64.StdEncoding.EncodeToString(wrong.Sum()),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("since") == "" {
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListRepoOpsOutput{Ops: ops, Cursor: rev1})
			return
		}
		_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListRepoOpsOutput{
			Ops:    []habitat.NetworkHabitatSpaceListRepoOpsOpEntry{},
			Commit: commit,
		})
	}))
	t.Cleanup(srv.Close)

	resyncer, db := setupResyncerFor(t, srv.URL)
	space := habitat_syntax.SpaceURI(spaceURI)
	repoDID := syntax.DID("did:plc:member1")
	require.NoError(t, db.Create(&managedRepo{Space: space, DID: repoDID, State: RepoStateResyncing}).Error)

	require.NoError(t, resyncer.syncRepo(t.Context(), space, repoDID))

	var repo managedRepo
	require.NoError(t, db.Where("space = ? AND did = ?", space, repoDID).First(&repo).Error)
	require.Equal(t, RepoStateDesynced, repo.State)
	require.Equal(t, syntax.TID(""), repo.Rev)
	require.Nil(t, repo.Hash)
}

// TestResyncer_RecoverRepo checks that a desynced repo is rebuilt from a full
// com.atproto.space.getRepo CAR: the recovered records reach the outbox and the
// repo goes active with the verified hash. The CAR is produced by the host's own
// spaces.SerializeRepoCAR so the parser is exercised against the real format.
func TestResyncer_RecoverRepo(t *testing.T) {
	t.Parallel()

	// Build a real host-format getRepo CAR from a spaces store.
	spacesStore := testutil.NewTestStore(t)
	owner := syntax.DID("did:plc:testorg")
	groupType := syntax.NSID("network.habitat.group")
	uri, err := spacesStore.CreateSpace(t.Context(), owner, owner, groupType, "recover-space")
	require.NoError(t, err)
	collection := syntax.NSID("network.habitat.test")
	for j := range 3 {
		_, _, err := spacesStore.PutRecord(
			t.Context(), uri, owner, collection,
			syntax.RecordKey(fmt.Sprintf("k%d", j)), map[string]any{"n": int64(j)},
		)
		require.NoError(t, err)
	}
	blocks, err := spacesStore.ListRecordBlocks(t.Context(), uri, owner)
	require.NoError(t, err)
	require.Len(t, blocks, 3)

	rev, hash, found, err := spacesStore.RepoHead(t.Context(), uri, owner)
	require.NoError(t, err)
	require.True(t, found)
	commit := spacecommit.SignedCommit{Ver: spacecommit.Version, Hash: hash, Rev: rev}
	carBytes, err := spaces.SerializeRepoCAR(commit, blocks)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/xrpc/com.atproto.space.getRepo", r.URL.Path)
		w.Header().Set("Content-Type", "application/vnd.ipld.car")
		_, _ = w.Write(carBytes)
	}))
	t.Cleanup(srv.Close)

	resyncer, db := setupResyncerFor(t, srv.URL)
	require.NoError(t, db.Create(&managedRepo{
		Space: uri, DID: owner, State: RepoStateDesynced, Rev: syntax.TID("stalerev"),
	}).Error)

	require.NoError(t, resyncer.recoverRepo(t.Context(), uri, owner))

	var repo managedRepo
	require.NoError(t, db.Where("space = ? AND did = ?", uri, owner).First(&repo).Error)
	require.Equal(t, RepoStateActive, repo.State)
	require.Equal(t, syntax.TID(rev), repo.Rev)
	require.NotNil(t, repo.Hash)

	var records []outbox
	require.NoError(t, db.Find(&records).Error)
	require.Len(t, records, 3)
}

// TestResyncer_RecoverRepoHashMismatch checks that a getRepo CAR whose commit
// hash disagrees with its records is rejected: the repo is left non-active and
// scheduled for retry rather than being trusted.
func TestResyncer_RecoverRepoHashMismatch(t *testing.T) {
	t.Parallel()

	spacesStore := testutil.NewTestStore(t)
	owner := syntax.DID("did:plc:testorg")
	groupType := syntax.NSID("network.habitat.group")
	uri, err := spacesStore.CreateSpace(t.Context(), owner, owner, groupType, "recover-space")
	require.NoError(t, err)
	collection := syntax.NSID("network.habitat.test")
	_, _, err = spacesStore.PutRecord(
		t.Context(), uri, owner, collection, "k0", map[string]any{"n": int64(0)},
	)
	require.NoError(t, err)
	blocks, err := spacesStore.ListRecordBlocks(t.Context(), uri, owner)
	require.NoError(t, err)

	// A commit hash that does not match the records in the CAR.
	var wrong spacecommit.LtHash
	wrong.Add(spacecommit.RecordElement(collection, "k0", "bafyreidifferent"))
	rev, _, _, err := spacesStore.RepoHead(t.Context(), uri, owner)
	require.NoError(t, err)
	commit := spacecommit.SignedCommit{Ver: spacecommit.Version, Hash: wrong.Sum(), Rev: rev}
	carBytes, err := spaces.SerializeRepoCAR(commit, blocks)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.ipld.car")
		_, _ = w.Write(carBytes)
	}))
	t.Cleanup(srv.Close)

	resyncer, db := setupResyncerFor(t, srv.URL)
	require.NoError(t, db.Create(&managedRepo{
		Space: uri, DID: owner, State: RepoStateDesynced,
	}).Error)

	require.NoError(t, resyncer.recoverRepo(t.Context(), uri, owner))

	var repo managedRepo
	require.NoError(t, db.Where("space = ? AND did = ?", uri, owner).First(&repo).Error)
	require.NotEqual(t, RepoStateActive, repo.State)
	require.Positive(t, repo.RetryAfter)

	var records []outbox
	require.NoError(t, db.Find(&records).Error)
	require.Empty(t, records)
}

func TestResyncer_RunDispatchesPendingReposOnStartup(t *testing.T) {
	t.Parallel()
	clock := syntax.NewTIDClock(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/xrpc/network.habitat.space.listRepoOps", r.URL.Path)
		_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListRepoOpsOutput{
			Ops: []habitat.NetworkHabitatSpaceListRepoOpsOpEntry{
				{
					Rev:        clock.Next().String(),
					Collection: "network.habitat.note",
					Rkey:       "k1",
					Value:      map[string]any{"text": "hello"},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	db := openTestDB(t)
	resyncNotif := utils.NewPollNotifier()
	outboxNotif := utils.NewPollNotifier()
	store, err := oauthclient.NewGormStore(db)
	require.NoError(t, err)
	cfg := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)
	oauthApp := oauthclient.NewApp(&cfg, store)
	resyncBuf := newResyncBuffer(db, resyncNotif, outboxNotif)
	resyncer := newResyncer(db, oauthApp, resyncBuf, resyncNotif, outboxNotif, 1, newTestMetrics(t))

	tk := testJWT(t)
	require.NoError(t, store.SaveSession(t.Context(), oauth.ClientSessionData{
		AccountDID:  "did:plc:testorg",
		SessionID:   "sess1",
		HostURL:     srv.URL,
		AccessToken: tk,
	}))
	require.NoError(t, db.Create(&managedOrg{
		DID:       "did:plc:testorg",
		SessionID: "sess1",
	}).Error)

	space := habitat_syntax.SpaceURI("ats://did:plc:testorg/network.habitat.space/my-space")
	repoDID := syntax.DID("did:plc:member1")
	require.NoError(
		t,
		db.Create(&managedRepo{Space: space, DID: repoDID, State: RepoStatePending}).Error,
	)

	// No notification is sent on resyncNotifCh: the repo was left pending by
	// a prior process lifetime (e.g. a crash, or the dispatcher dropping its
	// one notification on an error), and run() must sweep for it on its own.
	go func() { resyncer.run(t.Context()) }()

	require.Eventually(t, func() bool {
		var repo managedRepo
		require.NoError(t, db.Where("space = ? AND did = ?", space, repoDID).First(&repo).Error)
		return repo.State == RepoStateActive
	}, 10*time.Second, 100*time.Millisecond)
}

func TestResyncer_Dispatcher(t *testing.T) {
	t.Parallel()
	clock := syntax.NewTIDClock(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/xrpc/network.habitat.space.listRepoOps", r.URL.Path)
		_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListRepoOpsOutput{
			Ops: []habitat.NetworkHabitatSpaceListRepoOpsOpEntry{
				{
					Rev:        clock.Next().String(),
					Collection: "network.habitat.note",
					Rkey:       "k1",
					Value:      map[string]any{"text": "hello"},
				},
				{
					Rev:        clock.Next().String(),
					Collection: "network.habitat.note",
					Rkey:       "k2",
					Value:      map[string]any{"text": "hello"},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	db := openTestDB(t)
	resyncNotif := utils.NewPollNotifier()
	outboxNotif := utils.NewPollNotifier()
	store, err := oauthclient.NewGormStore(db)
	require.NoError(t, err)
	cfg := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)
	oauthApp := oauthclient.NewApp(&cfg, store)
	resyncBuf := newResyncBuffer(db, resyncNotif, outboxNotif)
	resyncer := newResyncer(
		db,
		oauthApp,
		resyncBuf,
		resyncNotif,
		outboxNotif,
		10,
		newTestMetrics(t),
	)

	tk := testJWT(t)
	require.NoError(t, store.SaveSession(t.Context(), oauth.ClientSessionData{
		AccountDID:  "did:plc:testorg",
		SessionID:   "sess1",
		HostURL:     srv.URL,
		AccessToken: tk,
	}))
	require.NoError(t, db.Create(&managedOrg{
		DID:       "did:plc:testorg",
		SessionID: "sess1",
	}).Error)

	for i := range 10 {
		for j := range 10 {
			require.NoError(t, db.
				Create(
					&managedRepo{
						Space: habitat_syntax.SpaceURI(fmt.Sprintf(
							"ats://did:plc:testorg/network.habitat.space/space-%d",
							i,
						)),
						DID:   syntax.DID(fmt.Sprintf("did:plc:member-%d", j)),
						Rev:   clock.Next(),
						State: RepoStatePending,
					},
				).Error)
		}
	}

	resyncNotif.Notify()

	go func() { resyncer.run(t.Context()) }()

	require.Eventually(t, func() bool {
		var records []outbox
		require.NoError(t, db.Find(&records).Error)
		t.Logf("records: %d", len(records))
		return len(records) == 200
	}, 10*time.Second, 100*time.Millisecond,
	)
}
