package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"

	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/pear"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	"github.com/libp2p/go-libp2p/p2p/transport/websocket"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/metric"
)

type peerRegistry struct {
	mu sync.RWMutex

	// Map of record (gossipsub topics) --> peerID currently subscribed --> channel on which to notify peer of new peers
	peersByTopic map[habitat_syntax.HabitatURI]map[peer.ID]chan peer.ID
}

func newPeerRegistry() *peerRegistry {
	return &peerRegistry{
		mu:           sync.RWMutex{},
		peersByTopic: make(map[habitat_syntax.HabitatURI]map[peer.ID]chan peer.ID),
	}
}

func (pr *peerRegistry) register(topic habitat_syntax.HabitatURI, peerID peer.ID) chan peer.ID {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	if _, ok := pr.peersByTopic[topic]; !ok {
		pr.peersByTopic[topic] = make(map[peer.ID]chan peer.ID)
	}
	ch := make(chan peer.ID)
	pr.peersByTopic[topic][peerID] = ch
	return ch
}

// TODO: there's no reverse map of peer --> subscribed topic so we have to check all of them
// This could be optimized by adding a reverse map.
//
// It's also unclear whether multiple tabs == multiple peers or if different tabs share a peerID for the same client.
// We need to make sure we support both cases.
func (pr *peerRegistry) deregister(peerID peer.ID) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	for topic, peers := range pr.peersByTopic {
		ch, ok := peers[peerID]
		if ok {
			close(ch)
			delete(peers, peerID)
			if len(peers) == 0 {
				delete(pr.peersByTopic, topic)
			}
		}
	}
}

func (pr *peerRegistry) peers(topic habitat_syntax.HabitatURI) []string {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	result := make([]string, 0, len(pr.peersByTopic[topic]))
	for peerID := range pr.peersByTopic[topic] {
		result = append(result, peerID.String())
	}
	return result
}

func (pr *peerRegistry) Disconnected(_ network.Network, conn network.Conn) {
	pr.deregister(conn.RemotePeer())
}
func (pr *peerRegistry) Listen(network.Network, ma.Multiaddr)      {}
func (pr *peerRegistry) ListenClose(network.Network, ma.Multiaddr) {}
func (pr *peerRegistry) Connected(network.Network, network.Conn)   {}

func (pr *peerRegistry) notifySubscribedPeers(topic habitat_syntax.HabitatURI, peerID peer.ID) {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	for other, ch := range pr.peersByTopic[topic] {
		// Don't send to self
		if other != peerID {
			select {
			case ch <- peerID:
			default:
			}
		}
	}
}

type Server struct {
	host     host.Host
	proxy    *httputil.ReverseProxy
	registry *peerRegistry

	// For authn/authz
	serviceAuth authn.Method
	pear        pear.Pear

	// Count the open conns on this server
	conns      atomic.Int64
	connsGauge metric.Int64Gauge
}

var _ io.Closer = (*Server)(nil)

func NewServer(serviceAuth authn.Method, pear pear.Pear, meter metric.Meter) (*Server, error) {
	host, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0/ws"), // Websocket for browser relay
		libp2p.Transport(websocket.New),
		libp2p.ForceReachabilityPublic(),
		libp2p.EnableRelayService(relay.WithResources((relay.DefaultResources()))),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
	}
	log.Info().Msgf("p2p server peer id: %s", host.ID())

	addr, err := manet.ToNetAddr(host.Addrs()[0])
	if err != nil {
		return nil, fmt.Errorf("failed to convert multiaddr to net.Addr: %w", err)
	}
	url, err := url.Parse(addr.String())
	if err != nil {
		return nil, fmt.Errorf("failed to parse url: %w", err)
	}
	url.Scheme = "http"

	registry := newPeerRegistry()
	// Notify the registry about disconnections
	host.Network().Notify(registry)

	gauge, err := meter.Int64Gauge("p2p.connections", metric.WithUnit("item"))
	if err != nil {
		return nil, err
	}

	s := &Server{
		host:        host,
		proxy:       httputil.NewSingleHostReverseProxy(url),
		registry:    registry,
		serviceAuth: serviceAuth,
		pear:        pear,
		connsGauge:  gauge,
	}

	type discoveryRequest struct {
		Topic            string `json:"topic"`
		ServiceAuthToken string `json:"serviceauth_token"`
	}

	const peerDiscoveryProtocol = "/habitat/peer-discovery/1.0.0"
	host.SetStreamHandler(peerDiscoveryProtocol, func(stream network.Stream) {
		peerID := stream.Conn().RemotePeer()

		// Context tied to this stream handler
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		// Close the stream upon return
		defer func() { _ = stream.Reset() /* ignore errors on stream close */ }()

		var req discoveryRequest
		if err := json.NewDecoder(stream).Decode(&req); err != nil {
			log.Error().
				Err(err).
				Str("peer", peerID.String()).
				Msg("peer-discovery: failed to decode request")
			return
		}

		// TODO: validate req.Credential and check topic permissions
		topic, err := habitat_syntax.ParseHabitatURI(req.Topic)
		if err != nil {
			return
		}

		// Always use service auth here -- the service is p2p peer discovery for habitat.
		did, authn, err := s.serviceAuth.ValidateRaw(ctx, req.ServiceAuthToken)
		if err != nil || !authn {
			// Ignore this peer -- don't let it know about others and don't let others discover it
			return
		}

		authz, err := s.pear.HasPermission(
			ctx,
			did,
			did,
			topic.Authority().DID(),
			topic.Collection(),
			topic.RecordKey(),
		)
		if err != nil || !authz {
			// Ignore this peer -- don't let it know about others and don't let others discover it
			return
		}

		ch := registry.register(topic, peerID)
		defer registry.deregister(peerID)

		registry.notifySubscribedPeers(topic, peerID)

		// Send existing peers
		for _, id := range registry.peers(topic) {
			if _, err := fmt.Fprintf(stream, "%s\n", id); err != nil {
				return
			}
		}

		for {
			id, ok := <-ch
			// channel is closed = deregistered peer.
			if !ok {
				break
			}
			// error writing to stream = closed; return.
			if _, err := fmt.Fprintf(stream, "%s\n", id); err != nil {
				break
			}
		}
	})

	return s, nil
}

func (s *Server) HandleLibp2p(w http.ResponseWriter, r *http.Request) {
	conns := s.conns.Add(1)
	s.connsGauge.Record(context.Background(), conns)

	defer func() {
		conns := s.conns.Add(-1)
		s.connsGauge.Record(context.Background(), conns)
	}()
	// just forward to libp2p
	s.proxy.ServeHTTP(w, r)
}

// Close implements io.Closer.
func (s *Server) Close() error {
	return s.host.Close()
}
