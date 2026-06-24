package sap

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCrawler(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/xrpc/network.habitat.space.listSpaces":
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListSpacesOutput{
				Spaces: []habitat.NetworkHabitatSpaceListSpacesSpaceView{
					{
						Uri: fmt.Sprintf(
							"ats://%s/network.habitat.space/my-space",
							r.Header.Get("Authorization")[len("Bearer "):],
						),
						Type: "network.habitat.space",
					},
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
	resyncNotif := utils.NewPollNotifier()
	outboxNotif := utils.NewPollNotifier()
	orgManager := newOrgManager(db, "", nil, nil)
	resyncBuf := newResyncBuffer(db, resyncNotif, outboxNotif)
	sub := newSubscriber(db, orgManager, resyncBuf)
	crawler := newCrawler(db, orgManager, resyncBuf, sub, resyncNotif)

	require.NoError(t, db.Create(&managedOrg{
		DID:         "did:plc:testorg",
		Host:        srv.URL,
		AccessToken: "did:plc:testorg",
		ExpiresAt:   time.Now().Add(time.Hour),
	}).Error)

	require.NoError(t, db.Create(&managedOrg{
		DID:         "did:plc:testorg2",
		Host:        srv.URL,
		AccessToken: "did:plc:testorg2",
		ExpiresAt:   time.Now().Add(time.Hour),
		CrawlState:  new(crawlStateRunning),
	}).Error)

	require.NoError(t, crawler.resumeIncompleteCrawls(t.Context()))

	require.Eventually(t, func() bool {
		var orgs []managedOrg
		require.NoError(t, db.Find(&orgs).Error)
		t.Logf("discovered: %v", orgs[0].CrawlState)
		return orgs[0].CrawlState != nil &&
			*orgs[0].CrawlState == crawlStateComplete &&
			orgs[1].CrawlState != nil &&
			*orgs[1].CrawlState == crawlStateComplete
	}, 1*time.Second, 10*time.Millisecond)

	var discovered []managedRepo
	require.NoError(t, db.Find(&discovered).Error)
	require.Len(t, discovered, 4)
	require.Equal(t, RepoStatePending, discovered[0].State)
	require.Equal(t, RepoStatePending, discovered[1].State)
}

func TestCrawler_Error(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/xrpc/network.habitat.space.listSpaces":
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListSpacesOutput{
				Spaces: []habitat.NetworkHabitatSpaceListSpacesSpaceView{
					{
						Uri: fmt.Sprintf(
							"ats://%s/network.habitat.space/my-space",
							r.Header.Get("Authorization")[len("Bearer "):],
						),
						Type: "network.habitat.space",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	db := openTestDB(t)
	resyncNotif := utils.NewPollNotifier()
	outboxNotif := utils.NewPollNotifier()
	orgManager := newOrgManager(db, "", nil, nil)
	resyncBuf := newResyncBuffer(db, resyncNotif, outboxNotif)
	sub := newSubscriber(db, orgManager, resyncBuf)
	crawler := newCrawler(db, orgManager, resyncBuf, sub, resyncNotif)

	require.NoError(t, db.Create(&managedOrg{
		DID:         "did:plc:testorg",
		Host:        srv.URL,
		AccessToken: "did:plc:testorg",
		ExpiresAt:   time.Now().Add(time.Hour),
	}).Error)

	require.NoError(t, crawler.resumeIncompleteCrawls(t.Context()))

	var org managedOrg
	require.Eventually(t, func() bool {
		require.NoError(t, db.First(&org).Error)
		return org.CrawlState != nil && *org.CrawlState == crawlStateErrored
	}, 1*time.Second, 10*time.Millisecond)

	require.NotEmpty(t, org.ErrorMsg)
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/test.db?_journal_mode=WAL"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, autoMigrate(db))
	return db
}
