package p2p

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/p2p/transport/websocket"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/rs/zerolog/log"
)

type Server struct {
	host        host.Host
	proxy       *httputil.ReverseProxy
	connections *expirable.LRU[string, []string]
}

var _ io.Closer = (*Server)(nil)

func NewServer() (*Server, error) {
	host, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0/ws"),
		libp2p.Transport(websocket.New),
		libp2p.ForceReachabilityPublic(),
		libp2p.EnableRelayService(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
	}
	log.Info().Msgf("peer id: %s", host.ID())

	ps, err := pubsub.NewGossipSub(context.Background(), host)
	if err != nil {
		return nil, fmt.Errorf("failed to create pubsub: %w", err)
	}
	topic, err := ps.Join("test")
	if err != nil {
		return nil, fmt.Errorf("failed to join pubsub topic: %w", err)
	}
	_, err = topic.Relay()
	if err != nil {
		return nil, fmt.Errorf("failed to relay pubsub topic: %w", err)
	}
	addr, err := manet.ToNetAddr(host.Addrs()[0])
	if err != nil {
		return nil, fmt.Errorf("failed to convert multiaddr to net.Addr: %w", err)
	}
	url, err := url.Parse(addr.String())
	if err != nil {
		return nil, fmt.Errorf("failed to parse url: %w", err)
	}
	url.Scheme = "http"
	return &Server{
		host:        host,
		proxy:       httputil.NewSingleHostReverseProxy(url),
		connections: expirable.NewLRU[string, []string](1024, nil, time.Hour*2),
	}, nil
}

func (s *Server) HandleLibp2p(w http.ResponseWriter, r *http.Request) {
	// just forward to libp2p
	s.proxy.ServeHTTP(w, r)
}

// Close implements io.Closer.
func (s *Server) Close() error {
	return s.host.Close()
}
