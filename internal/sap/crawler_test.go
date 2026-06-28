package sap

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/oauth_client"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/stretchr/testify/require"
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

	store, err := oauth_client.NewGormStore(db)
	require.NoError(t, err)
	cfg := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)
	oauthApp := oauth_client.NewApp(&cfg, store)

	resyncBuf := newResyncBuffer(db, resyncNotif, outboxNotif)
	sub := newSubscriber(db, oauthApp, resyncBuf)
	crawler := newCrawler(db, oauthApp, resyncBuf, sub, resyncNotif)

	require.NoError(t, store.SaveSession(t.Context(), oauth.ClientSessionData{
		AccountDID:  "did:plc:testorg",
		SessionID:   "sess1",
		HostURL:     srv.URL,
		AccessToken: "did:plc:testorg",
	}))
	require.NoError(t, db.Create(&managedOrg{
		DID:       "did:plc:testorg",
		SessionID: "sess1",
	}).Error)

	require.NoError(t, store.SaveSession(t.Context(), oauth.ClientSessionData{
		AccountDID:  "did:plc:testorg2",
		SessionID:   "sess2",
		HostURL:     srv.URL,
		AccessToken: "did:plc:testorg2",
	}))
	require.NoError(t, db.Create(&managedOrg{
		DID:        "did:plc:testorg2",
		SessionID:  "sess2",
		CrawlState: new(crawlStateRunning),
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

	store, err := oauth_client.NewGormStore(db)
	require.NoError(t, err)
	cfg := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)
	oauthApp := oauth_client.NewApp(&cfg, store)

	resyncBuf := newResyncBuffer(db, resyncNotif, outboxNotif)
	sub := newSubscriber(db, oauthApp, resyncBuf)
	crawler := newCrawler(db, oauthApp, resyncBuf, sub, resyncNotif)

	require.NoError(t, store.SaveSession(t.Context(), oauth.ClientSessionData{
		AccountDID:  "did:plc:testorg",
		SessionID:   "sess1",
		HostURL:     srv.URL,
		AccessToken: "did:plc:testorg",
	}))
	require.NoError(t, db.Create(&managedOrg{
		DID:       "did:plc:testorg",
		SessionID: "sess1",
	}).Error)

	require.NoError(t, crawler.resumeIncompleteCrawls(t.Context()))

	var org managedOrg
	require.Eventually(t, func() bool {
		require.NoError(t, db.First(&org).Error)
		return org.CrawlState != nil && *org.CrawlState == crawlStateErrored
	}, 1*time.Second, 10*time.Millisecond)

	require.NotEmpty(t, org.ErrorMsg)
}
