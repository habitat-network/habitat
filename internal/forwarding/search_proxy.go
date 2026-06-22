package forwarding

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

// NewSearchProxy returns a reverse proxy that forwards requests unchanged
// (including the original Authorization header) to the search server at
// searchHost. Unlike serviceProxy/pdsForwarding, no signing or auth
// validation happens here — the search server validates the caller's token
// itself via pear's existing org.getMetadata endpoint.
func NewSearchProxy(searchHost string) (http.Handler, error) {
	target, err := url.Parse(searchHost)
	if err != nil {
		return nil, err
	}
	return httputil.NewSingleHostReverseProxy(target), nil
}
