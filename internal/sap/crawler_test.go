package sap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/events"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCrawler_DiscoverRepos(t *testing.T) {
	t.Parallel()

	spaceURI := "ats://did:plc:testorg/network.habitat.space/my-space"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/xrpc/network.habitat.space.listSpaces":
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListSpacesOutput{
				Spaces: []habitat.NetworkHabitatSpaceListSpacesSpaceView{
					{Uri: spaceURI, Type: "network.habitat.space"},
				},
			})
		case "/xrpc/network.habitat.space.getMembers":
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceGetMembersOutput{
				Members: []habitat.NetworkHabitatSpaceGetMembersMember{
					{Did: "did:plc:member1"},
					{Did: "did:plc:member2"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	db := openTestDB(t)
	orgManager := newOrgManager(db, "", nil)
	repos := newRepoManager(db)
	resyncBuf := newResyncBuffer(db, repos)
	crawler := newCrawler(db, orgManager, repos, resyncBuf)

	org := &managedOrg{
		DID:         "did:plc:testorg",
		Host:        srv.URL,
		AccessToken: "token",
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	require.NoError(t, db.Create(org).Error)

	require.NoError(t, crawler.resumeCrawl(t.Context(), org))

	var discovered []managedRepo
	require.NoError(t, db.Find(&discovered).Error)
	require.Len(t, discovered, 2)
	require.Equal(t, RepoStatePending, discovered[0].State)
	require.Equal(t, RepoStatePending, discovered[1].State)
}

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
	orgManager := newOrgManager(db, "", nil)
	repos := newRepoManager(db)
	resyncBuf := newResyncBuffer(db, repos)
	resyncer := newResyncer(db, orgManager, repos, resyncBuf, 1)

	require.NoError(t, db.Create(&managedOrg{
		DID:         "did:plc:testorg",
		Host:        srv.URL,
		AccessToken: "token",
		ExpiresAt:   time.Now().Add(time.Hour),
	}).Error)

	space := habitat_syntax.SpaceURI(spaceURI)
	repoDID := syntax.DID("did:plc:member1")
	require.NoError(t, repos.EnsureRepo(context.Background(), space, repoDID))
	require.NoError(t, db.Model(&managedRepo{}).
		Where("space = ? AND did = ?", space, repoDID).
		Update("state", RepoStateResyncing).Error)

	require.NoError(t, resyncer.syncRepo(context.Background(), space, repoDID))

	var records []outbox
	require.NoError(t, db.Find(&records).Error)
	require.Len(t, records, 2)

	repo, err := repos.GetRepo(context.Background(), space, repoDID)
	require.NoError(t, err)
	require.Equal(t, RepoStateActive, repo.State)
	require.Equal(t, syntax.TID(rev2), repo.Rev)
}

func TestResyncBuffer_AppendAndDrain(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	repos := newRepoManager(db)
	resyncBuf := newResyncBuffer(db, repos)

	space := habitat_syntax.SpaceURI("ats://did:plc:testorg/network.habitat.space/my-space")
	repoDID := syntax.DID("did:plc:member1")
	require.NoError(t, repos.EnsureRepo(context.Background(), space, repoDID))

	clock := syntax.NewTIDClock(0)
	rev := clock.Next()
	recordURI := habitat_syntax.SpaceRecordURI(
		"ats://did:plc:testorg/network.habitat.space/my-space/did:plc:member1/network.habitat.note/k1",
	)
	event := events.Event{
		Seq:   1,
		Space: space,
		Repo:  repoDID,
		Rev:   rev,
		Ops: []events.EventOps{
			{
				Uri:    recordURI,
				Action: "create",
				Value:  map[string]any{"text": "hello"},
			},
		},
	}

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return resyncBuf.appendTx(tx, event)
	}))

	require.NoError(t, repos.SetActive(context.Background(), space, repoDID, rev))
	require.NoError(t, resyncBuf.drainRepo(context.Background(), space, repoDID))

	var records []outbox
	require.NoError(t, db.Find(&records).Error)
	require.Len(t, records, 1)

	var buffered []bufferedEvent
	require.NoError(t, db.Find(&buffered).Error)
	require.Len(t, buffered, 0)
}

func TestRepoManager_ClaimForResync(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	repos := newRepoManager(db)
	space := habitat_syntax.SpaceURI("ats://did:plc:testorg/network.habitat.space/my-space")

	require.NoError(t, repos.EnsureRepo(context.Background(), space, "did:plc:a"))
	require.NoError(t, repos.EnsureRepo(context.Background(), space, "did:plc:b"))

	_, _, found, err := repos.ClaimForResync(context.Background(), RepoStatePending)
	require.NoError(t, err)
	require.True(t, found)

	_, _, found, err = repos.ClaimForResync(context.Background(), RepoStatePending)
	require.NoError(t, err)
	require.True(t, found)

	_, _, found, err = repos.ClaimForResync(context.Background(), RepoStatePending)
	require.NoError(t, err)
	require.False(t, found)
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/test.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, autoMigrate(db))
	return db
}
