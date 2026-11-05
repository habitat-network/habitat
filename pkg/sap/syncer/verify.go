package syncer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"

	"github.com/habitat-network/habitat/internal/spacecommit"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Verifier authenticates what a repo host serves sap. Commits and records
// carry no signature: for each request sap generates an ephemeral key, derives
// a shared secret against the repo's KEM key, and sends the key share in an
// Atproto-Space-Want-Mac header; the host answers with a MAC over the commit
// or record under that secret (see spacecommit.Encapsulate).
//
// The KEM key is the author's own #atproto_kem when the host being read is
// the author's PDS, as for habitat-managed authors. Habitat also hosts space
// repos for authors whose identity lives on a PDS it does not control, and so
// whose DID document it cannot publish a key in; for those the host keys MACs
// with its own #atproto_kem, published in the host's did:web document. pear
// makes the same choice (see spacecommit.KEMKeys).
type Verifier struct {
	dir identity.Directory
}

// NewVerifier builds a Verifier. A nil directory (or nil Verifier) degrades to
// hash-only verification: no MAC is requested or checked, so a commit's data
// is compared with the recomputed state but not authenticated.
func NewVerifier(dir identity.Directory) *Verifier {
	return &Verifier{dir: dir}
}

// macExchange is one MAC-bearing request: the Want-Mac header it sends and
// the shared secret the response's MAC must be keyed by. A nil exchange is the
// degraded, unauthenticated mode.
type macExchange struct {
	typ    string
	header string
	secret []byte
}

// wantMAC is the request's Atproto-Space-Want-Mac header value, empty in
// degraded mode.
func (x *macExchange) wantMAC() string {
	if x == nil {
		return ""
	}
	return x.header
}

// verifyCommit checks that c names space and repo and that the response's
// MAC authenticates it. It says nothing about whether c's data matches local
// state; see [checkData].
func (x *macExchange) verifyCommit(
	h http.Header,
	c spacecommit.Commit,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
) error {
	if x == nil {
		return nil
	}
	mac, err := spacecommit.ParseMAC(h, x.typ)
	if err != nil {
		return err
	}
	return c.Verify(space, repo, x.secret, mac)
}

// verifyRecord checks that the response's MAC authenticates a record's URI
// and CID.
func (x *macExchange) verifyRecord(h http.Header, uri string, c cid.Cid) error {
	if x == nil {
		return nil
	}
	mac, err := spacecommit.ParseMAC(h, x.typ)
	if err != nil {
		return err
	}
	return spacecommit.VerifyRecordMAC(x.secret, uri, c, mac)
}

// checkData compares an authenticated commit's data with the locally folded
// state. Returns an error wrapping spacecommit.ErrInvalidCommit on a mismatch.
func checkData(c spacecommit.Commit, lt *spacecommit.LtHash) error {
	if !c.Matches(lt) {
		return fmt.Errorf("%w: hash mismatch", spacecommit.ErrInvalidCommit)
	}
	return nil
}

// isMACFailure reports whether err means a MAC did not verify, or the host
// refused the key share: either can follow a KEM key rotation sap has not
// seen yet.
func isMACFailure(err error) bool {
	var apiErr *atclient.APIError
	return errors.Is(err, spacecommit.ErrInvalidCommit) ||
		errors.Is(err, spacecommit.ErrBadSpaceMAC) ||
		(errors.As(err, &apiErr) && apiErr.Name == "BadSpaceMac")
}

// withMAC runs one MAC-bearing request about author's repo at host, with a
// fresh exchange. If its MAC fails, the KEM key may have rotated: the cached
// DID documents are dropped and the request retried once with a new exchange.
// A repeated failure is returned, still wrapping the MAC error.
func (v *Verifier) withMAC(
	ctx context.Context,
	host string,
	author syntax.DID,
	typ string,
	run func(x *macExchange) error,
) error {
	var err error
	for attempt := range 2 {
		var x *macExchange
		x, err = v.exchange(ctx, host, author, typ)
		if err != nil {
			return err
		}
		err = run(x)
		if x == nil || !isMACFailure(err) || attempt > 0 {
			return err
		}
		v.refresh(ctx, host, author)
	}
	return err
}

// exchange encapsulates a new shared secret for a request about author's
// repo at host. Each request gets its own, including retries and each page of
// a paginated response.
func (v *Verifier) exchange(
	ctx context.Context,
	host string,
	author syntax.DID,
	typ string,
) (*macExchange, error) {
	if v == nil || v.dir == nil {
		return nil, nil
	}
	kem, err := v.kemKey(ctx, host, author)
	if err != nil {
		return nil, err
	}
	keyShare, secret, err := spacecommit.Encapsulate(kem)
	if err != nil {
		return nil, fmt.Errorf("encapsulate: %w", err)
	}
	header, err := spacecommit.FormatWantMAC(typ, keyShare)
	if err != nil {
		return nil, err
	}
	return &macExchange{typ: typ, header: header, secret: secret}, nil
}

// kemKey resolves the KEM key a request about author's repo at host is keyed
// by: the author's #atproto_kem when host is the author's PDS, otherwise the
// host's own (see [Verifier]).
func (v *Verifier) kemKey(
	ctx context.Context,
	host string,
	author syntax.DID,
) (atcrypto.PublicKey, error) {
	ident, err := v.dir.LookupDID(ctx, author)
	if err != nil {
		return nil, fmt.Errorf("lookup author %s: %w", author, err)
	}
	if sameHost(ident.PDSEndpoint(), host) {
		key, err := ident.GetPublicKey(spacecommit.KEMFragment)
		if err != nil {
			return nil, fmt.Errorf("author %s kem key: %w", author, err)
		}
		return key, nil
	}
	hostDID, err := hostDID(host)
	if err != nil {
		return nil, err
	}
	hostIdent, err := v.dir.LookupDID(ctx, hostDID)
	if err != nil {
		return nil, fmt.Errorf("lookup host %s: %w", hostDID, err)
	}
	key, err := hostIdent.GetPublicKey(spacecommit.KEMFragment)
	if err != nil {
		return nil, fmt.Errorf("host %s kem key: %w", hostDID, err)
	}
	return key, nil
}

// refresh drops the cached DID documents a request's KEM key was resolved
// from, so the next resolution sees a rotated key.
func (v *Verifier) refresh(ctx context.Context, host string, author syntax.DID) {
	_ = v.dir.Purge(ctx, author.AtIdentifier())
	if did, err := hostDID(host); err == nil {
		_ = v.dir.Purge(ctx, did.AtIdentifier())
	}
}

// hostDID is the did:web of a habitat host serving at endpoint. did:web
// encodes a port's colon as %3A.
func hostDID(endpoint string) (syntax.DID, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("parse host url %q: %w", endpoint, err)
	}
	return syntax.DID("did:web:" + strings.ReplaceAll(u.Host, ":", "%3A")), nil
}

// sameHost reports whether two service endpoints name the same host.
func sameHost(a, b string) bool {
	ua, errA := url.Parse(a)
	ub, errB := url.Parse(b)
	return errA == nil && errB == nil && ua.Host != "" && strings.EqualFold(ua.Host, ub.Host)
}

// getXRPC performs one XRPC query with an optional Atproto-Space-Want-Mac
// header, decoding a JSON response into out (or copying the raw body into a
// *bytes.Buffer out), and returns the response headers so the caller can read
// the MAC. Non-2xx responses are returned as *atclient.APIError, like
// atclient.APIClient.Get.
func getXRPC(
	ctx context.Context,
	client *atclient.APIClient,
	endpoint syntax.NSID,
	params map[string]any,
	wantMAC string,
	out any,
) (http.Header, error) {
	req := atclient.NewAPIRequest(http.MethodGet, endpoint, nil)
	if _, raw := out.(*bytes.Buffer); !raw {
		req.Headers.Set("Accept", "application/json")
	}
	if wantMAC != "" {
		req.Headers.Set(spacecommit.WantMACHeader, wantMAC)
	}
	qp, err := atclient.ParseParams(params)
	if err != nil {
		return nil, err
	}
	req.QueryParams = qp

	resp, err := client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var eb atclient.ErrorBody
		if err := json.NewDecoder(resp.Body).Decode(&eb); err != nil {
			return nil, &atclient.APIError{StatusCode: resp.StatusCode}
		}
		return nil, eb.APIError(resp.StatusCode)
	}
	if buf, raw := out.(*bytes.Buffer); raw {
		if _, err := io.Copy(buf, resp.Body); err != nil {
			return nil, fmt.Errorf("read response body: %w", err)
		}
		return resp.Header, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return nil, fmt.Errorf("decode response body: %w", err)
	}
	return resp.Header, nil
}

// recordCID computes the CID of a record's JSON value, so a fetched value can
// be checked against the CID its MAC authenticated.
func recordCID(value any) (cid.Cid, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return cid.Cid{}, err
	}
	data, err := atdata.UnmarshalJSON(raw)
	if err != nil {
		return cid.Cid{}, err
	}
	encoded, err := atdata.MarshalCBOR(data)
	if err != nil {
		return cid.Cid{}, err
	}
	return cid.NewPrefixV1(cid.DagCBOR, multihash.SHA2_256).Sum(encoded)
}
