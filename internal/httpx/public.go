package httpx

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// ErrNonPublicAddress is returned when a PublicClient is asked to connect to an
// address that is not on the public internet.
var ErrNonPublicAddress = errors.New("refusing to connect to a non-public address")

// sharedAddressSpace is RFC 6598's carrier-grade NAT range, which netip
// doesn't count as private but which is not reachable from the internet either.
var sharedAddressSpace = netip.MustParsePrefix("100.64.0.0/10")

// NewPublicClient returns an instrumented *http.Client, like NewClient, that
// only connects to public internet addresses. Use it for requests to URLs that
// users supply, such as an org's MCP servers, so they can't reach loopback,
// private-network or link-local addresses (e.g. the cloud metadata server)
// from inside pear.
//
// The check runs on the address actually dialed, after DNS resolution, so a
// hostname that resolves (or rebinds) to a private address is refused too.
// The client ignores proxy environment variables, since dialing a proxy would
// bypass the check.
func NewPublicClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   denyNonPublic,
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dialer.DialContext
	return &http.Client{Transport: otelhttp.NewTransport(transport)}
}

// denyNonPublic is a net.Dialer Control func refusing any address that isn't
// public unicast.
func denyNonPublic(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrNonPublicAddress, address)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrNonPublicAddress, address)
	}
	if !IsPublicAddr(ip) {
		return fmt.Errorf("%w: %s", ErrNonPublicAddress, ip)
	}
	return nil
}

// IsPublicAddr reports whether ip is a unicast address on the public
// internet: not loopback, private, link-local, multicast, unspecified or
// carrier-grade NAT.
func IsPublicAddr(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !sharedAddressSpace.Contains(ip)
}
