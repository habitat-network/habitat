package spacecommit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/dunglas/httpsfv"
	"github.com/ipfs/go-cid"
)

// Space repos authenticate commits and records with a MAC rather than a
// signature, so a leaked response proves nothing to a third party. A MAC is an
// HTTP message signature (RFC 9421) using hmac-sha256, keyed by a per-request
// shared secret (see [Encapsulate] and [Decapsulate]).
//
// The syncer requests a MAC with an Accept-Signature header whose tag names a
// profile, with the key share as its keyid:
//
//	Accept-Signature: mac=("atproto-space-commit";sf);keyid="did:key:zDna...";tag="atproto-space-commit"
//
// The host answers with a header describing the commit or record it returned,
// and a MAC covering that header:
//
//	Atproto-Space-Commit: ver=1, repo="did:...", space="at://...", rev="...", data=:...:
//	Signature-Input: mac=("atproto-space-commit";sf);keyid="did:key:zDna...";tag="atproto-space-commit"
//	Signature: mac=:<padded standard base64>:
//	Vary: Accept-Signature
const (
	// AcceptSignatureHeader is the request header a syncer asks for a MAC with.
	AcceptSignatureHeader = "Accept-Signature"

	// CommitMACType is the profile (and tag) of a commit's MAC, covering the
	// Atproto-Space-Commit header (getLatestCommit, listRepoOps, getRepo).
	CommitMACType = "atproto-space-commit"
	// RecordMACType is the profile (and tag) of a record's MAC, covering the
	// Atproto-Space-Record header (getRecord).
	RecordMACType = "atproto-space-record"

	macAlg = "hmac-sha256"
	// macLabel is the label a syncer requests its MAC under; a host answers
	// under whichever label it was requested with.
	macLabel = "mac"

	signatureInputHeader = "Signature-Input"
	signatureHeader      = "Signature"
)

// ErrInvalidSpaceMAC reports a missing or malformed MAC request or response
// header, an unsupported profile or algorithm, or an invalid key share. Repo
// hosts answer it as the InvalidSpaceMac XRPC error.
var ErrInvalidSpaceMAC = errors.New("invalid space mac")

// coveredHeader is the header a profile's MAC covers, which is also the
// profile's name.
func coveredHeader(profile string) string { return profile }

// componentList is the covered component list of a profile's MAC: its header,
// flagged sf as a structured field.
func componentList(profile string) string {
	return `("` + coveredHeader(profile) + `";sf)`
}

// FormatMACRequest builds the Accept-Signature request header value asking for
// profile's MAC keyed by keyShare.
func FormatMACRequest(profile, keyShare string) string {
	return fmt.Sprintf(
		`%s=%s;keyid=%q;tag=%q`, macLabel, componentList(profile), keyShare, profile,
	)
}

// MACRequest is a MAC request as a host reads it from Accept-Signature.
type MACRequest struct {
	Label    string
	KeyShare string
	// SignatureParams is the requested signature parameters, serialized. The
	// host's MAC uses them as given.
	SignatureParams string
}

// ParseMACRequest finds the MAC requested for profile in a request's
// Accept-Signature headers and checks the host can fulfill it. Requests with
// other tags are ignored: it returns nil with no error when none is for
// profile. Any other problem wraps [ErrInvalidSpaceMAC].
func ParseMACRequest(h http.Header, profile string) (*MACRequest, error) {
	values := h.Values(AcceptSignatureHeader)
	if len(values) == 0 {
		return nil, nil
	}
	d, err := httpsfv.UnmarshalDictionary(values)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: malformed %s header: %w",
			ErrInvalidSpaceMAC,
			AcceptSignatureHeader,
			err,
		)
	}
	label, params, err := memberWithTag(d, profile)
	if err != nil || params == nil {
		return nil, err
	}
	if got := serializeComponents(params.Items); got != componentList(profile) {
		return nil, fmt.Errorf(
			"%w: MAC must cover exactly %s",
			ErrInvalidSpaceMAC,
			componentList(profile),
		)
	}
	for _, name := range params.Params.Names() {
		if name != "keyid" && name != "tag" && name != "alg" {
			return nil, fmt.Errorf("%w: unsupported MAC parameter: %s", ErrInvalidSpaceMAC, name)
		}
	}
	if alg, ok := params.Params.Get("alg"); ok && alg != macAlg {
		return nil, fmt.Errorf("%w: MAC algorithm must be %s", ErrInvalidSpaceMAC, macAlg)
	}
	keyShare, ok := params.Params.Get("keyid")
	keyShareStr, isString := keyShare.(string)
	if !ok || !isString || !strings.HasPrefix(keyShareStr, "did:") {
		return nil, fmt.Errorf("%w: missing or malformed keyid", ErrInvalidSpaceMAC)
	}
	serialized, err := httpsfv.Marshal(*params)
	if err != nil {
		return nil, fmt.Errorf("serialize signature params: %w", err)
	}
	return &MACRequest{Label: label, KeyShare: keyShareStr, SignatureParams: serialized}, nil
}

// SetCommitMAC sets a commit's response headers: Atproto-Space-Commit
// describing it, and a MAC over that header requested by req and keyed by
// sharedSecret. It also exposes the headers to browser apps. A host must list
// Accept-Signature in Vary on every response that can carry a MAC, with or
// without one.
func SetCommitMAC(h http.Header, c Commit, req MACRequest, sharedSecret []byte) error {
	d := httpsfv.NewDictionary()
	d.Add("ver", httpsfv.NewItem(int64(c.Ver)))
	d.Add("repo", httpsfv.NewItem(c.Repo.String()))
	d.Add("space", httpsfv.NewItem(c.Space.String()))
	d.Add("rev", httpsfv.NewItem(c.Rev))
	d.Add("data", httpsfv.NewItem(c.Data))
	return setMAC(h, CommitMACType, d, req, sharedSecret)
}

// SetRecordMAC sets a record's response headers: Atproto-Space-Record naming
// its URI and CID, and a MAC over that header. See [SetCommitMAC].
func SetRecordMAC(h http.Header, uri string, c cid.Cid, req MACRequest, sharedSecret []byte) error {
	d := httpsfv.NewDictionary()
	d.Add("uri", httpsfv.NewItem(uri))
	d.Add("cid", httpsfv.NewItem(c.String()))
	return setMAC(h, RecordMACType, d, req, sharedSecret)
}

func setMAC(
	h http.Header,
	profile string,
	covered *httpsfv.Dictionary,
	req MACRequest,
	sharedSecret []byte,
) error {
	value, err := httpsfv.Marshal(covered)
	if err != nil {
		return fmt.Errorf("format %s header: %w", coveredHeader(profile), err)
	}
	mac := hmacSHA256(sharedSecret, signatureBase(profile, value, req.SignatureParams))
	h.Set(coveredHeader(profile), value)
	h.Set(signatureInputHeader, req.Label+"="+req.SignatureParams)
	h.Set(signatureHeader, req.Label+"=:"+base64.StdEncoding.EncodeToString(mac)+":")
	h.Add(
		"Access-Control-Expose-Headers",
		coveredHeader(profile)+", "+signatureInputHeader+", "+signatureHeader,
	)
	return nil
}

// VerifyCommitMAC checks a response's MAC, and that the Atproto-Space-Commit
// header it covers describes c, in constant time. keyShare and sharedSecret are
// those the request was made with. A malformed or missing header wraps
// [ErrInvalidSpaceMAC]; a mismatch wraps [ErrInvalidCommit].
func VerifyCommitMAC(h http.Header, c Commit, keyShare string, sharedSecret []byte) error {
	fields, err := verifyMAC(h, CommitMACType, keyShare, sharedSecret)
	if err != nil {
		return err
	}
	data, _ := fields["data"].([]byte)
	if fields["ver"] != int64(c.Ver) ||
		fields["repo"] != c.Repo.String() ||
		fields["space"] != c.Space.String() ||
		fields["rev"] != c.Rev ||
		!hmac.Equal(data, c.Data) {
		return fmt.Errorf("%w: commit does not match its mac", ErrInvalidCommit)
	}
	return nil
}

// VerifyRecordMAC checks a response's MAC, and that the Atproto-Space-Record
// header it covers names uri and c. See [VerifyCommitMAC].
func VerifyRecordMAC(
	h http.Header,
	uri string,
	c cid.Cid,
	keyShare string,
	sharedSecret []byte,
) error {
	fields, err := verifyMAC(h, RecordMACType, keyShare, sharedSecret)
	if err != nil {
		return err
	}
	if fields["uri"] != uri || fields["cid"] != c.String() {
		return fmt.Errorf("%w: record does not match its mac", ErrInvalidCommit)
	}
	return nil
}

// verifyMAC checks the MAC tagged profile in h and returns the fields of the
// header it covers, as bare values.
func verifyMAC(
	h http.Header,
	profile, keyShare string,
	sharedSecret []byte,
) (map[string]any, error) {
	for _, name := range []string{signatureInputHeader, signatureHeader, coveredHeader(profile)} {
		if len(h.Values(name)) == 0 {
			return nil, fmt.Errorf("%w: missing %s header", ErrInvalidSpaceMAC, name)
		}
	}
	inputs, err := httpsfv.UnmarshalDictionary(h.Values(signatureInputHeader))
	if err != nil {
		return nil, fmt.Errorf(
			"%w: malformed %s header: %w",
			ErrInvalidSpaceMAC,
			signatureInputHeader,
			err,
		)
	}
	label, params, err := memberWithTag(inputs, profile)
	if err != nil {
		return nil, err
	}
	if params == nil {
		return nil, fmt.Errorf("%w: missing signature with tag %q", ErrInvalidSpaceMAC, profile)
	}
	if got := serializeComponents(params.Items); got != componentList(profile) {
		return nil, fmt.Errorf(
			"%w: MAC must cover exactly %s",
			ErrInvalidSpaceMAC,
			componentList(profile),
		)
	}
	if alg, ok := params.Params.Get("alg"); ok && alg != macAlg {
		return nil, fmt.Errorf("%w: MAC algorithm must be %s", ErrInvalidSpaceMAC, macAlg)
	}
	if id, ok := params.Params.Get("keyid"); ok && id != keyShare {
		return nil, fmt.Errorf("%w: MAC keyid does not match the key share", ErrInvalidSpaceMAC)
	}
	sigs, err := httpsfv.UnmarshalDictionary(h.Values(signatureHeader))
	if err != nil {
		return nil, fmt.Errorf(
			"%w: malformed %s header: %w",
			ErrInvalidSpaceMAC,
			signatureHeader,
			err,
		)
	}
	member, _ := sigs.Get(label)
	item, isItem := member.(httpsfv.Item)
	sig, isBytes := item.Value.([]byte)
	if !isItem || !isBytes {
		return nil, fmt.Errorf("%w: missing or malformed signature %q", ErrInvalidSpaceMAC, label)
	}

	covered, err := httpsfv.UnmarshalDictionary(h.Values(coveredHeader(profile)))
	if err != nil {
		return nil, fmt.Errorf(
			"%w: malformed %s header: %w",
			ErrInvalidSpaceMAC,
			coveredHeader(profile),
			err,
		)
	}
	// The sf flag covers the header as re-serialized from its parsed form.
	value, err := httpsfv.Marshal(covered)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: malformed %s header: %w",
			ErrInvalidSpaceMAC,
			coveredHeader(profile),
			err,
		)
	}
	serializedParams, err := httpsfv.Marshal(*params)
	if err != nil {
		return nil, fmt.Errorf("serialize signature params: %w", err)
	}
	want := hmacSHA256(sharedSecret, signatureBase(profile, value, serializedParams))
	if !hmac.Equal(want, sig) {
		return nil, fmt.Errorf("%w: mac mismatch", ErrInvalidCommit)
	}

	fields := make(map[string]any, len(covered.Names()))
	for _, name := range covered.Names() {
		if m, _ := covered.Get(name); m != nil {
			if it, ok := m.(httpsfv.Item); ok {
				fields[name] = it.Value
			}
		}
	}
	return fields, nil
}

// memberWithTag finds the one inner-list member of d tagged tag, returning its
// label, or nil params when there is none. More than one wraps
// [ErrInvalidSpaceMAC].
func memberWithTag(d *httpsfv.Dictionary, tag string) (string, *httpsfv.InnerList, error) {
	var label string
	var found *httpsfv.InnerList
	for _, name := range d.Names() {
		member, _ := d.Get(name)
		il, ok := member.(httpsfv.InnerList)
		if !ok || il.Params == nil {
			continue
		}
		if t, _ := il.Params.Get("tag"); t != tag {
			continue
		}
		if found != nil {
			return "", nil, fmt.Errorf("%w: more than one MAC with tag %q", ErrInvalidSpaceMAC, tag)
		}
		label, found = name, &il
	}
	return label, found, nil
}

// serializeComponents renders a covered component list without its parameters.
func serializeComponents(items []httpsfv.Item) string {
	s, err := httpsfv.Marshal(httpsfv.InnerList{Items: items, Params: httpsfv.NewParams()})
	if err != nil {
		return ""
	}
	return s
}

// signatureBase is the RFC 9421 section 2.5 signature base of a MAC covering
// profile's header.
func signatureBase(profile, headerValue, signatureParams string) []byte {
	return []byte(fmt.Sprintf(
		"%q;sf: %s\n\"@signature-params\": %s",
		coveredHeader(profile), strings.TrimSpace(headerValue), signatureParams,
	))
}

// hmacSHA256 is HMAC-SHA256(key, data).
func hmacSHA256(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	_, _ = m.Write(data)
	return m.Sum(nil)
}
