package testutil

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/stretchr/testify/require"
)

type reqOpts struct {
	p       *TestPear
	headers http.Header
}

func WithHeader(key string, value string) utils.Opt[reqOpts] {
	return func(opts *reqOpts) {
		opts.headers.Add(key, value)
	}
}

func WithOAuth(user syntax.DID) utils.Opt[reqOpts] {
	return func(opts *reqOpts) {
		token, err := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.RegisteredClaims{
			Subject: user.String(),
		}).SignedString(opts.p.OAuthSigningKey)
		require.NoError(opts.p.T, err)
		WithHeader("Authorization", "Bearer "+token)(opts)
	}
}

func passthrough(desired reqOpts) utils.Opt[reqOpts] {
	return func(curr *reqOpts) {
		*curr = desired
	}
}

func (p *TestPear) Do(r *http.Request, opts ...utils.Opt[reqOpts]) *httptest.ResponseRecorder {
	opt := utils.ResolveOptions(reqOpts{p: p}, opts)
	r.Header = opt.headers
	w := httptest.NewRecorder()
	p.Handler().ServeHTTP(w, r)
	return w
}

func (p *TestPear) XRPC(r *http.Request, out any,
	opts ...utils.Opt[reqOpts],
) {
	resp := p.Do(r, opts...)
	require.NoError(p.T, json.Unmarshal(resp.Body.Bytes(), out))
}

func (p *TestPear) XRPCProcedure(
	nsid string,
	input any,
	out any,
	opts ...utils.Opt[reqOpts],
) {
	body, err := json.Marshal(input)
	require.NoError(p.T, err)
	r := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/"+nsid,
		bytes.NewReader(body),
	)
	p.XRPC(r, out, opts...)
}

func (p *TestPear) XRPCQuery(
	nsid string,
	input url.Values,
	out any,
	opts ...utils.Opt[reqOpts],
) {
	r := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/"+nsid+"?"+input.Encode(),
		http.NoBody,
	)
	p.XRPC(r, out, opts...)
}
