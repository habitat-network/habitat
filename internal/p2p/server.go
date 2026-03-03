package p2p

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"github.com/bradenaw/juniper/xmaps"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	"github.com/libp2p/go-libp2p/p2p/transport/websocket"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/rs/zerolog/log"
)

// peerRegistry maps gossipsub topic → set of peer ID strings.
// Entries are removed automatically when a peer disconnects.
// It also tracks active discovery stream subscriptions for push notifications.
type peerRegistry struct {
	mu      sync.RWMutex
	entries map[habitat_syntax.HabitatURI]xmaps.Set[peer.ID]
	subs    map[habitat_syntax.HabitatURI][]chan string
}

func newPeerRegistry() *peerRegistry {
	return &peerRegistry{
		entries: make(map[habitat_syntax.HabitatURI]xmaps.Set[peer.ID]),
		subs:    make(map[habitat_syntax.HabitatURI][]chan string),
	}
}

func (pr *peerRegistry) register(topic habitat_syntax.HabitatURI, peerID peer.ID) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	if _, ok := pr.entries[topic]; !ok {
		pr.entries[topic] = xmaps.Set[peer.ID]{}
	}
	pr.entries[topic].Add(peerID)
}

func (pr *peerRegistry) remove(peerID peer.ID) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	for topic, peers := range pr.entries {
		delete(peers, peerID)
		if len(peers) == 0 {
			delete(pr.entries, topic)
		}
	}
}

func (pr *peerRegistry) peers(topic habitat_syntax.HabitatURI) []string {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	result := make([]string, 0, len(pr.entries[topic]))
	for peerID := range pr.entries[topic] {
		result = append(result, peerID.String())
	}
	return result
}

func (pr *peerRegistry) Disconnected(_ network.Network, conn network.Conn) {
	pr.remove(conn.RemotePeer())
}
func (pr *peerRegistry) Listen(network.Network, ma.Multiaddr)      {}
func (pr *peerRegistry) ListenClose(network.Network, ma.Multiaddr) {}
func (pr *peerRegistry) Connected(network.Network, network.Conn) {}

func (pr *peerRegistry) subscribe(topic habitat_syntax.HabitatURI) chan string {
	ch := make(chan string, 16)
	pr.mu.Lock()
	pr.subs[topic] = append(pr.subs[topic], ch)
	pr.mu.Unlock()
	return ch
}

func (pr *peerRegistry) unsubscribe(topic habitat_syntax.HabitatURI, ch chan string) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	list := pr.subs[topic]
	for i, c := range list {
		if c == ch {
			pr.subs[topic] = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(pr.subs[topic]) == 0 {
		delete(pr.subs, topic)
	}
}

func (pr *peerRegistry) notify(topic habitat_syntax.HabitatURI, peerID string) {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	for _, ch := range pr.subs[topic] {
		select {
		case ch <- peerID:
		default:
		}
	}
}

type Server struct {
	host     host.Host
	proxy    *httputil.ReverseProxy
	registry *peerRegistry
}

var _ io.Closer = (*Server)(nil)

func NewServer() (*Server, error) {
	host, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0/ws"), // Websocket for browser relay
		libp2p.Transport(websocket.New),
		libp2p.ForceReachabilityPublic(),
		libp2p.EnableRelayService(relay.WithResources((relay.DefaultResources()))),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
	}
	log.Info().Msgf("peer id: %s", host.ID())

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

	const peerDiscoveryProtocol = "/habitat/peer-discovery/1.0.0"
	host.SetStreamHandler(peerDiscoveryProtocol, func(stream network.Stream) {
		buf := make([]byte, 4096)
		n, err := stream.Read(buf)
		if err != nil {
			stream.Reset() //nolint:errcheck
			return
		}
		topic, err := habitat_syntax.ParseHabitatURI(strings.TrimSpace(string(buf[:n])))
		if err != nil {
			stream.Reset() //nolint:errcheck
			return
		}

		ch := registry.subscribe(topic)
		defer registry.unsubscribe(topic, ch)

		// Send existing peers
		for _, id := range registry.peers(topic) {
			if _, err := fmt.Fprintf(stream, "%s\n", id); err != nil {
				stream.Reset() //nolint:errcheck
				return
			}
		}

		// Signal when read side closes (browser half-close or disconnect)
		done := make(chan struct{})
		go func() { io.Copy(io.Discard, stream); close(done) }() //nolint:errcheck

		for {
			select {
			case id := <-ch:
				if _, err := fmt.Fprintf(stream, "%s\n", id); err != nil {
					stream.Reset() //nolint:errcheck
					return
				}
			case <-done:
				stream.Close() //nolint:errcheck
				return
			}
		}
	})

	return &Server{
		host:     host,
		proxy:    httputil.NewSingleHostReverseProxy(url),
		registry: registry,
	}, nil
}

func (s *Server) HandleLibp2p(w http.ResponseWriter, r *http.Request) {
	// just forward to libp2p
	s.proxy.ServeHTTP(w, r)
}

type registerRequest struct {
	PeerID string `json:"peerId"`
	Topic  string `json:"topic"`
}

type peersResponse struct {
	Peers []string `json:"peers"`
}

// HandlePeers serves per-document peer discovery.
//
// POST /p2p/peers  body: {"peerId":"...","topic":"..."}
//
//	Register the calling browser as a participant for the given topic.
//
// GET  /p2p/peers?topic=<topic>
//
//	Return peer IDs of all browsers registered for that topic.
//
// Entries are removed automatically when the underlying libp2p connection drops.
func (s *Server) HandlePeers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		topic, err := habitat_syntax.ParseHabitatURI(r.URL.Query().Get("topic"))
		if err != nil {
			utils.LogAndHTTPError(w, err, "topic is not a valid habitat uri", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(peersResponse{Peers: s.registry.peers(topic)})
		if err != nil {
			utils.LogAndHTTPError(w, err, "encoding response", http.StatusInternalServerError)
			return
		}

	case http.MethodPost:
		var req registerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.PeerID == "" || req.Topic == "" {
			http.Error(w, "peerId and topic are required", http.StatusBadRequest)
			return
		}

		topic, err := habitat_syntax.ParseHabitatURI(req.Topic)
		if err != nil {
			utils.LogAndHTTPError(w, err, "topic is not a valid habitat uri", http.StatusBadRequest)
			return
		}

		peerID, err := peer.Decode(req.PeerID)
		if err != nil {
			utils.LogAndHTTPError(w, err, "peerID is not valid", http.StatusBadRequest)
		}

		s.registry.register(topic, peerID)
		s.registry.notify(topic, req.PeerID)
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// Close implements io.Closer.
func (s *Server) Close() error {
	return s.host.Close()
}
