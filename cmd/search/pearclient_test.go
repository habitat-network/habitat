package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/events"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/stretchr/testify/require"
)

func TestPearClient_ResolveCallerOrg_ForwardsCallerToken(t *testing.T) {
	var gotAuth, gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotMethod = r.Header.Get("Habitat-Auth-Method")
		require.Equal(t, "/xrpc/network.habitat.org.getMetadata", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatOrgGetMetadataOutput{
			OrgId:           "did:plc:org1",
			LoginMethod:     "password",
			HandleSubdomain: "org1",
		})
	}))
	defer server.Close()

	client := newPearClient(server.URL, "m2m-secret")
	orgDID, err := client.ResolveCallerOrg(context.Background(), "callers-own-token")
	require.NoError(t, err)
	require.Equal(t, "did:plc:org1", orgDID.String())
	require.Equal(t, "Bearer callers-own-token", gotAuth)
	require.Equal(t, "oauth", gotMethod)
}

func TestPearClient_ResolveCallerOrg_NonOKStatusIsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := newPearClient(server.URL, "m2m-secret")
	_, err := client.ResolveCallerOrg(context.Background(), "bad-token")
	require.Error(t, err)
}

func TestPearClient_SubscribeSpaces_DecodesSpaceEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer m2m-secret", r.Header.Get("Authorization"))
		require.Equal(t, "oauth", r.Header.Get("Habitat-Auth-Method"))

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte("id: 1\nevent: space\ndata: {\"seq\":1,\"type\":\"space\",\"space\":\"ats://did:plc:org1/app.space/skey1\"}\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	client := newPearClient(server.URL, "m2m-secret")

	ctx, cancel := context.WithCancel(context.Background())
	received := make(chan events.Event, 1)
	go func() {
		_ = client.SubscribeSpaces(ctx, 0, func(event events.Event) {
			received <- event
			cancel()
		})
	}()

	select {
	case event := <-received:
		require.Equal(t, uint64(1), event.Seq)
		require.Equal(t, habitat_syntax.SpaceURI("ats://did:plc:org1/app.space/skey1"), event.Space)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}
