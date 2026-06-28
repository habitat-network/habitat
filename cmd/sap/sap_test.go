package main

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/habitat-network/habitat/internal/sap"
	"github.com/habitat-network/habitat/internal/oauth_client"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSap_Start(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/sap.db?_journal_mode=WAL"), &gorm.Config{})
	require.NoError(t, err)

	store, err := oauth_client.NewGormStore(db)
	require.NoError(t, err)

	cfg := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)
	oauthApp := oauth_client.NewApp(&cfg, store)

	s, err := sap.NewSap(sap.SapConfig{
		PublicDomain: "example.com",
		Secret:       "z42tt1ZWxkfKn5ujwLsELfY7191h4q6UCFjeRGf6tKXaMCnX",
		DB:           db,
		OAuthClient:  oauthApp,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	go func() {
		if err := s.Start(ctx); err != nil {
			t.Logf("Start returned: %v", err)
		}
	}()

	cancel()
}
