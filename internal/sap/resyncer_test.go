package sap

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
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
	resyncNotifCh := make(chan struct{}, 1)
	orgManager := newOrgManager(db, "", nil, nil)
	resyncBuf := newResyncBuffer(db, resyncNotifCh)
	resyncer := newResyncer(db, orgManager, resyncBuf, resyncNotifCh, 1)

	require.NoError(t, db.Create(&managedOrg{
		DID:         "did:plc:testorg",
		Host:        srv.URL,
		AccessToken: "token",
		ExpiresAt:   time.Now().Add(time.Hour),
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
	resyncNotifCh := make(chan struct{}, 1)
	orgManager := newOrgManager(db, "", nil, nil)
	resyncBuf := newResyncBuffer(db, resyncNotifCh)
	resyncer := newResyncer(db, orgManager, resyncBuf, resyncNotifCh, 10)

	require.NoError(t, db.Create(&managedOrg{
		DID:         "did:plc:testorg",
		Host:        srv.URL,
		AccessToken: "token",
		ExpiresAt:   time.Now().Add(time.Hour),
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

	resyncNotifCh <- struct{}{}

	go func() { resyncer.run(t.Context()) }()

	require.Eventually(t, func() bool {
		var records []outbox
		require.NoError(t, db.Find(&records).Error)
		t.Logf("records: %d", len(records))
		return len(records) == 200
	}, 10*time.Second, 100*time.Millisecond,
	)
}
