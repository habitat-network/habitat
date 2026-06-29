package sap

import (
	"context"
	"sync"
	"testing"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSap_Start(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/test.db"), &gorm.Config{})
	require.NoError(t, err)

	store, err := oauthclient.NewGormStore(db)
	require.NoError(t, err)

	cfg := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)
	oauthApp := oauthclient.NewApp(&cfg, store)

	s, err := NewSap(SapConfig{
		PublicDomain: "https://example.com",
		Secret:       "z42tt1ZWxkfKn5ujwLsELfY7191h4q6UCFjeRGf6tKXaMCnX",
		DB:           db,
		OAuthClient:  oauthApp,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		require.ErrorIs(t, s.Start(ctx), context.Canceled)
	}()

	cancel()

	wg.Wait()
}
