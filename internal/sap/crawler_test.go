package sap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCrawler_DiscoverRepos(t *testing.T) {
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
	resyncNotifCh := make(chan struct{}, 1)
	orgManager := newOrgManager(db, "", nil, nil)
	resyncBuf := newResyncBuffer(db, resyncNotifCh)
	sub := newSubscriber(db, orgManager, resyncBuf)
	crawler := newCrawler(db, orgManager, resyncBuf, sub, resyncNotifCh)

	org := &managedOrg{
		DID:         "did:plc:testorg",
		Host:        srv.URL,
		AccessToken: "token",
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	require.NoError(t, db.Create(org).Error)

	crawler.crawlOrg(t.Context(), org)

	require.NoError(t, db.First(&org).Error)
	require.Equal(t, crawlStateComplete, *org.CrawlState)

	var discovered []managedRepo
	require.NoError(t, db.Find(&discovered).Error)
	require.Len(t, discovered, 2)
	require.Equal(t, RepoStatePending, discovered[0].State)
	require.Equal(t, RepoStatePending, discovered[1].State)
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/test.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, autoMigrate(db))
	return db
}
