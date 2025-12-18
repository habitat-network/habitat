package p2p

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	"github.com/libp2p/go-libp2p/p2p/transport/websocket"
	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

func setupServer(t *testing.T) (multiaddr.Multiaddr, *Server) {
	p2pServer, err := NewServer()
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(p2pServer.HandleLibp2p))
	serverUrl, err := url.Parse(server.URL)
	require.NoError(t, err)
	ma, err := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/127.0.0.1/tcp/%s/ws", serverUrl.Port()))
	require.NoError(t, err)
	return ma, p2pServer
}

func TestP2PPing(t *testing.T) {
	ma, p2pServer := setupServer(t)
	defer p2pServer.Close()

	// Create a separate test client host
	clientHost, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0/ws"),
		libp2p.Transport(websocket.New),
	)
	require.NoError(t, err)
	defer clientHost.Close()

	serverId := p2pServer.host.ID()

	// Create peer info for privi host
	priviAddrInfo := peer.AddrInfo{
		ID:    serverId,
		Addrs: []multiaddr.Multiaddr{ma},
	}

	// Connect the client to the privi host
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = clientHost.Connect(ctx, priviAddrInfo)
	require.NoError(t, err, "client should successfully connect to privi host")

	// Create a ping service
	pingService := ping.NewPingService(clientHost)

	// Ping the privi host
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()

	resultChan := pingService.Ping(pingCtx, serverId)

	// Wait for ping result
	select {
	case result := <-resultChan:
		require.NoError(t, result.Error, "ping should succeed without error")
		require.Greater(t, result.RTT, time.Duration(0), "RTT should be greater than 0")
		t.Logf("Ping successful! RTT: %v", result.RTT)
	case <-pingCtx.Done():
		t.Fatal("ping timed out")
	}
}

func TestRelay(t *testing.T) {
	ma, p2pServer := setupServer(t)
	defer p2pServer.Close()

	// Create a separate test client host
	client1, err := libp2p.New(
		libp2p.NoListenAddrs,
		libp2p.Transport(websocket.New),
		libp2p.EnableRelay(),
		libp2p.EnableAutoRelayWithStaticRelays([]peer.AddrInfo{
			{
				ID:    p2pServer.host.ID(),
				Addrs: []multiaddr.Multiaddr{ma},
			},
		}),
	)
	require.NoError(t, err)
	defer client1.Close()

	// Create a separate test client host
	client2, err := libp2p.New(
		libp2p.NoListenAddrs,
		libp2p.Transport(websocket.New),
		libp2p.EnableRelay(),
		libp2p.EnableAutoRelayWithStaticRelays([]peer.AddrInfo{
			{
				ID:    p2pServer.host.ID(),
				Addrs: []multiaddr.Multiaddr{ma},
			},
		}),
	)
	require.NoError(t, err)
	defer client2.Close()

	result := <-ping.NewPingService(client1).Ping(t.Context(), client2.ID())
	require.Error(t, result.Error, "ping should fail before relay connections")

	// Manually connect both clients to the relay and make reservations
	ctx := context.Background()
	relayAddrInfo := peer.AddrInfo{
		ID:    p2pServer.host.ID(),
		Addrs: []multiaddr.Multiaddr{ma},
	}

	err = client1.Connect(ctx, relayAddrInfo)
	require.NoError(t, err)

	err = client2.Connect(ctx, relayAddrInfo)
	require.NoError(t, err)

	// Manually make reservations on the relay
	_, err = client.Reserve(ctx, client1, relayAddrInfo)
	require.NoError(t, err, "client1 should reserve slot on relay")

	_, err = client.Reserve(ctx, client2, relayAddrInfo)
	require.NoError(t, err, "client2 should reserve slot on relay")

	// Add relay circuit address for client2 to client1's peerstore
	relayAddr, err := multiaddr.NewMultiaddr(
		fmt.Sprintf("/p2p/%s/p2p-circuit/p2p/%s", p2pServer.host.ID(), client2.ID()),
	)
	require.NoError(t, err)
	client1.Peerstore().AddAddr(client2.ID(), relayAddr, time.Hour)

	result = <-ping.NewPingService(client1).Ping(t.Context(), client2.ID())
	require.NoError(t, result.Error, "ping should succeed after relay connections")
}
