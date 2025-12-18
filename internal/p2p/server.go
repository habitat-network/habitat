package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/eagraf/habitat-new/internal/oauthserver"
	"github.com/eagraf/habitat-new/internal/utils"
	"github.com/gorilla/schema"
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
	oauthServer *oauthserver.OAuthServer
}

var _ io.Closer = (*Server)(nil)

func NewServer(oauthServer *oauthserver.OAuthServer) (*Server, error) {
	host, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0/ws"),
		libp2p.Transport(websocket.New),
		libp2p.ForceReachabilityPublic(),
		libp2p.EnableRelayService(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
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
	log.Info().Msgf("peer id: %s", host.ID())
	log.Info().Msgf("protocols: %v", host.Mux().Protocols())

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
	// _, err = topic.Subscribe()
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to subscribe to pubsub topic: %w", err)
	// }
	return &Server{
		host:        host,
		proxy:       httputil.NewSingleHostReverseProxy(url),
		connections: expirable.NewLRU[string, []string](1024, nil, time.Hour*2),
		oauthServer: oauthServer,
	}, nil
}

func (s *Server) HandleLibp2p(w http.ResponseWriter, r *http.Request) {
	s.proxy.ServeHTTP(w, r)
}

type discoverRequest struct {
	Addr string   `schema:"addr"`
	Dids []string `schema:"dids"`
}

var formDecoder = schema.NewDecoder()

func (s *Server) HandleDiscover(w http.ResponseWriter, r *http.Request) {
	callerDid, _, ok := s.oauthServer.Validate(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		utils.LogAndHTTPError(w, err, "parse form", http.StatusBadRequest)
		return
	}
	var req discoverRequest
	if err := formDecoder.Decode(&req, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(w, err, "decode json request", http.StatusBadRequest)
		return
	}
	calledConnections, _ := s.connections.Get(callerDid)
	if calledConnections == nil {
		calledConnections = []string{}
	}
	s.connections.Add(callerDid, append(calledConnections, req.Addr))

	addrs := map[string][]string{}
	if req.Dids == nil {
		for _, did := range s.connections.Keys() {
			didAddrs, _ := s.connections.Get(did)
			addrs[did] = didAddrs
		}
	} else {
		for _, did := range req.Dids {
			didAddrs, ok := s.connections.Get(did)
			if !ok {
				addrs[did] = []string{}
			} else {
				addrs[did] = didAddrs
			}
		}
	}
	err := json.NewEncoder(w).Encode(addrs)
	if err != nil {
		utils.LogAndHTTPError(w, err, "encode json response", http.StatusInternalServerError)
		return
	}
}

// Close implements io.Closer.
func (s *Server) Close() error {
	return s.host.Close()
}
