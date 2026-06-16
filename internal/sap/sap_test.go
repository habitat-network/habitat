package sap

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSap_Start(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/test.db"), &gorm.Config{})
	require.NoError(t, err)
	s, err := NewSap(SapConfig{
		PublicDomain: "https://example.com",
		Secret:       "z42tt1ZWxkfKn5ujwLsELfY7191h4q6UCFjeRGf6tKXaMCnX",
		DB:           db,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	var wg sync.WaitGroup
	wg.Go(func() {
		require.ErrorIs(t, s.Start(ctx), context.Canceled)
	})

	cancel()

	wg.Wait()
}
