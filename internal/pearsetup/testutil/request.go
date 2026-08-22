package testutil

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"

	"github.com/stretchr/testify/require"
)

// Do serves r through the full router — middleware, service proxy, and the
// real request validator — and returns the response. A nil or credential-less
// actor sends no Authorization header. The response body is closed when the
// test ends, so callers may read it without closing it themselves.
//
// Requests are served in-process with httptest rather than over a listener, so
// no port is bound and no goroutine outlives the test.
func (p *TestPear) Do(a *Actor, r *http.Request) *http.Response {
	p.T.Helper()

	if r.Host == "" {
		r.Host = Domain
	}
	if a != nil && a.Token != "" {
		r.Header.Set("Authorization", "Bearer "+a.Token)
	}

	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, r)

	resp := rec.Result()
	p.T.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// Query issues an XRPC query (GET) against nsid. When out is non-nil and the
// response is 200, the body is decoded into it.
func (p *TestPear) Query(a *Actor, nsid string, params url.Values, out any) *http.Response {
	p.T.Helper()

	target := "/xrpc/" + nsid
	if encoded := params.Encode(); encoded != "" {
		target += "?" + encoded
	}
	req := httptest.NewRequest(http.MethodGet, target, http.NoBody)

	resp := p.Do(a, req)
	p.decode(resp, out)
	return resp
}

// Procedure issues an XRPC procedure (POST) against nsid, JSON-encoding body
// when it is non-nil. When out is non-nil and the response is 200, the body is
// decoded into it.
func (p *TestPear) Procedure(a *Actor, nsid string, body, out any) *http.Response {
	p.T.Helper()

	var payload io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		require.NoError(p.T, err)
		payload = bytes.NewReader(encoded)
	}

	req := httptest.NewRequest(http.MethodPost, "/xrpc/"+nsid, payload)
	req.Header.Set("Content-Type", "application/json")

	resp := p.Do(a, req)
	p.decode(resp, out)
	return resp
}

// decode reads a successful response into out. Failures are left to the caller
// to assert on, so a test that expects an error status still sees its status
// rather than a decode failure.
func (p *TestPear) decode(resp *http.Response, out any) {
	p.T.Helper()

	if out == nil || resp.StatusCode != http.StatusOK {
		return
	}
	require.NoError(p.T, json.NewDecoder(resp.Body).Decode(out))
}
