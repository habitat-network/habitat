package sap

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/habitat-network/habitat/internal/oauth_client"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/clause"
)

func TestResyncer_SyncRepo(t *testing.T) {
	t.Parallel()

	spaceURI := "ats://did:plc:testorg/network.habitat.space/my-space"
	clock := syntax.NewTIDClock(0)
	rev1 := clock.Next().String()
	rev2 := clock.Next().String()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/xrpc/network.habitat.space.getRepoOplog", r.URL.Path)
		since := r.URL.Query().Get("since")
		switch since {
		case "":
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceGetRepoOplogOutput{
				Records: []habitat.NetworkHabitatSpaceGetRepoOplogRecord{
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
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceGetRepoOplogOutput{
				Records: []habitat.NetworkHabitatSpaceGetRepoOplogRecord{},
			})
		}
	}))
	t.Cleanup(srv.Close)

	db := openTestDB(t)
	resyncNotif := utils.NewPollNotifier()
	outboxNotif := utils.NewPollNotifier()
	store, err := oauth_client.NewGormStore(db)
	require.NoError(t, err)
	cfg := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)
	oauthApp := oauth_client.NewApp(&cfg, store)
	resyncBuf := newResyncBuffer(db, resyncNotif, outboxNotif)
	resyncer := newResyncer(db, oauthApp, resyncBuf, resyncNotif, outboxNotif, 1)

	require.NoError(t, store.SaveSession(t.Context(), oauth.ClientSessionData{
		AccountDID:  "did:plc:testorg",
		SessionID:   "sess1",
		HostURL:     srv.URL,
		AccessToken: "token",
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

func TestResyncer_RunDispatchesPendingReposOnStartup(t *testing.T) {
	t.Parallel()
	clock := syntax.NewTIDClock(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/xrpc/network.habitat.space.getRepoOplog", r.URL.Path)
		_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceGetRepoOplogOutput{
			Records: []habitat.NetworkHabitatSpaceGetRepoOplogRecord{
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
	store, err := oauth_client.NewGormStore(db)
	require.NoError(t, err)
	cfg := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)
	oauthApp := oauth_client.NewApp(&cfg, store)
	resyncBuf := newResyncBuffer(db, resyncNotif, outboxNotif)
	resyncer := newResyncer(db, oauthApp, resyncBuf, resyncNotif, outboxNotif, 1)

	require.NoError(t, store.SaveSession(t.Context(), oauth.ClientSessionData{
		AccountDID:  "did:plc:testorg",
		SessionID:   "sess1",
		HostURL:     srv.URL,
		AccessToken: "token",
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
		require.Equal(t, "/xrpc/network.habitat.space.getRepoOplog", r.URL.Path)
		_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceGetRepoOplogOutput{
			Records: []habitat.NetworkHabitatSpaceGetRepoOplogRecord{
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
	store, err := oauth_client.NewGormStore(db)
	require.NoError(t, err)
	cfg := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)
	oauthApp := oauth_client.NewApp(&cfg, store)
	resyncBuf := newResyncBuffer(db, resyncNotif, outboxNotif)
	resyncer := newResyncer(db, oauthApp, resyncBuf, resyncNotif, outboxNotif, 10)

	require.NoError(t, store.SaveSession(t.Context(), oauth.ClientSessionData{
		AccountDID:  "did:plc:testorg",
		SessionID:   "sess1",
		HostURL:     srv.URL,
		AccessToken: "token",
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
