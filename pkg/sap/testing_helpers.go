package sap

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func testJWT(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tok, err := jwt.NewWithClaims(jwt.SigningMethodPS256,
		jwt.MapClaims{"exp": time.Now().Add(time.Hour).Unix(), "jti": "test"},
	).SignedString(key)
	require.NoError(t, err)
	return tok
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.NewDB(t)
	require.NoError(t, autoMigrate(db))
	return db
}

func newTestMetrics(t *testing.T) *metrics {
	t.Helper()
	m, err := newMetrics(nil, nil)
	require.NoError(t, err)
	return m
}
