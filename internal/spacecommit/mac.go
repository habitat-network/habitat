package spacecommit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"

	"github.com/dunglas/httpsfv"
	"github.com/ipfs/go-cid"
)

// Space repos authenticate commits and records with a MAC rather than a
// signature. The MAC is keyed by a per-request shared secret (see
// [Encapsulate] and [Decapsulate]) and carried in HTTP headers rather than in
// the repo data:
//
//	Atproto-Space-Want-Mac: type="atproto-space-repo", ver=1, key_share="did:key:zDna..."
//	Atproto-Space-Mac: type="atproto-space-repo", ver=1, mac=:<padded standard base64>:
//	Vary: Atproto-Space-Want-Mac
const (
	// WantMACHeader is the request header carrying the syncer's key share.
	WantMACHeader = "Atproto-Space-Want-Mac"
	// MACHeader is the response header carrying the MAC.
	MACHeader = "Atproto-Space-Mac"

	// CommitMACType authenticates a commit (getLatestCommit, listRepoOps,
	// getRepo). Its version is the commit's own, [Version].
	CommitMACType = "atproto-space-repo"
	// RecordMACType authenticates a single record's URI and CID (getRecord).
	RecordMACType = "atproto-space-record"
	// RecordMACVersion is the version of [RecordMACType].
	RecordMACVersion = 1
)

// ErrBadSpaceMAC reports a missing or malformed MAC header, an unsupported MAC
// type or version, or an invalid key share. Repo hosts answer it as the
// BadSpaceMac XRPC error.
var ErrBadSpaceMAC = errors.New("bad space mac")

// macVersion is the version a MAC type is currently on.
func macVersion(typ string) int64 {
	if typ == CommitMACType {
		return Version
	}
	return RecordMACVersion
}

// RecordMAC computes a record's MAC under sharedSecret:
//
//	context = "atproto-space-record-v1" || uint16be(len(uri)) || uri
//	mac = HMAC-SHA256(shared_secret, context || record_cid_bytes)
//
// uri is the full space record URI, and the CID is in its binary form.
func RecordMAC(sharedSecret []byte, uri string, c cid.Cid) []byte {
	b := []byte(RecordMACType + "-v1")
	b = appendVec(b, []byte(uri))
	return hmacSHA256(sharedSecret, b, c.Bytes())
}

// VerifyRecordMAC checks mac against a record's URI and CID in constant time.
// Returns an error wrapping [ErrInvalidCommit] on a mismatch.
func VerifyRecordMAC(sharedSecret []byte, uri string, c cid.Cid, mac []byte) error {
	if !hmac.Equal(RecordMAC(sharedSecret, uri, c), mac) {
		return fmt.Errorf("%w: record mac mismatch", ErrInvalidCommit)
	}
	return nil
}

// FormatWantMAC builds the Atproto-Space-Want-Mac request header value.
func FormatWantMAC(typ, keyShare string) (string, error) {
	d := httpsfv.NewDictionary()
	d.Add("type", httpsfv.NewItem(typ))
	d.Add("ver", httpsfv.NewItem(macVersion(typ)))
	d.Add("key_share", httpsfv.NewItem(keyShare))
	return httpsfv.Marshal(d)
}

// ParseWantMAC parses an Atproto-Space-Want-Mac request header, checking its
// type and version, and returns its key share. A missing header returns
// ok=false with no error, so a caller can tell "not requested" (allowed on
// OAuth requests) from "malformed"; any other problem wraps [ErrBadSpaceMAC].
func ParseWantMAC(h http.Header, typ string) (keyShare string, ok bool, err error) {
	values := h.Values(WantMACHeader)
	if len(values) == 0 {
		return "", false, nil
	}
	fields, err := parseMACDictionary(values, typ)
	if err != nil {
		return "", true, err
	}
	keyShare, isString := fields["key_share"].(string)
	if !isString {
		return "", true, fmt.Errorf("%w: missing or malformed key_share", ErrBadSpaceMAC)
	}
	return keyShare, true, nil
}

// FormatMAC builds the Atproto-Space-Mac response header value.
func FormatMAC(typ string, mac []byte) (string, error) {
	d := httpsfv.NewDictionary()
	d.Add("type", httpsfv.NewItem(typ))
	d.Add("ver", httpsfv.NewItem(macVersion(typ)))
	d.Add("mac", httpsfv.NewItem(mac))
	return httpsfv.Marshal(d)
}

// SetMAC sets the Atproto-Space-Mac response header and lists the request
// header in Vary, as a repo host must on every MAC-bearing response.
func SetMAC(h http.Header, typ string, mac []byte) error {
	value, err := FormatMAC(typ, mac)
	if err != nil {
		return fmt.Errorf("format mac header: %w", err)
	}
	h.Set(MACHeader, value)
	h.Add("Vary", WantMACHeader)
	return nil
}

// ParseMAC parses an Atproto-Space-Mac response header, checking its type and
// version, and returns the MAC. Any problem, including a missing header,
// wraps [ErrBadSpaceMAC].
func ParseMAC(h http.Header, typ string) ([]byte, error) {
	values := h.Values(MACHeader)
	if len(values) == 0 {
		return nil, fmt.Errorf("%w: missing %s header", ErrBadSpaceMAC, MACHeader)
	}
	fields, err := parseMACDictionary(values, typ)
	if err != nil {
		return nil, err
	}
	mac, isBytes := fields["mac"].([]byte)
	if !isBytes {
		return nil, fmt.Errorf("%w: missing or malformed mac", ErrBadSpaceMAC)
	}
	return mac, nil
}

// parseMACDictionary parses either MAC header as a Structured Field
// Dictionary, checks its type and version, and flattens it to bare values.
func parseMACDictionary(values []string, typ string) (map[string]any, error) {
	d, err := httpsfv.UnmarshalDictionary(values)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed header: %w", ErrBadSpaceMAC, err)
	}
	fields := make(map[string]any, len(d.Names()))
	for _, name := range d.Names() {
		member, _ := d.Get(name)
		item, isItem := member.(httpsfv.Item)
		if !isItem {
			return nil, fmt.Errorf("%w: malformed header field %s", ErrBadSpaceMAC, name)
		}
		fields[name] = item.Value
	}
	if fields["type"] != typ {
		return nil, fmt.Errorf("%w: unsupported mac type, expected %s", ErrBadSpaceMAC, typ)
	}
	if fields["ver"] != macVersion(typ) {
		return nil, fmt.Errorf("%w: unsupported mac version for %s", ErrBadSpaceMAC, typ)
	}
	return fields, nil
}

// appendVec appends field to b length-prefixed with a big-endian uint16.
func appendVec(b, field []byte) []byte {
	var l [2]byte
	binary.BigEndian.PutUint16(l[:], uint16(len(field)))
	b = append(b, l[:]...)
	return append(b, field...)
}

// hmacSHA256 is HMAC-SHA256(key, context || data).
func hmacSHA256(key, context, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	_, _ = m.Write(context)
	_, _ = m.Write(data)
	return m.Sum(nil)
}
