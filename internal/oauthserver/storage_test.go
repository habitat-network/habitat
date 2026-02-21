package oauthserver

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/ory/fosite"
	"github.com/stretchr/testify/require"
)

func TestGetClient(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	secret, err := encrypt.GenerateKey()
	require.NoError(t, err)
	secretBytes, err := encrypt.ParseKey(secret)
	require.NoError(t, err)
	strat, err := newStrategy(
		secretBytes,
		&fosite.Config{})
	require.NoError(t, err)
	store := newStore(
		strat,
		db, // db not needed for this test
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("url: %s", r.Host)
		if r.URL.Path == "/client-metadata.json" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, err := fmt.Fprintf(w, `{
					"client_id": "%s"
				}`, "http://"+r.Host+r.URL.Path)
			require.NoError(t, err)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))

	clientId := server.URL + "/client-metadata.json"

	client, err := store.GetClient(context.Background(), clientId)
	require.NoError(t, err)

	require.Equal(t, clientId, client.GetID())
}
