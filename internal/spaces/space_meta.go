package spaces

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/star"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// SpaceMetaStarType is the STAR profile of a com.atproto.space.exportSpaceMeta
// archive.
const SpaceMetaStarType = "atproto-space-meta"

const (
	spaceMetaVersion = 1
	// maxSpaceMetaValue caps an entry's DRISL encoding, as the profile does.
	maxSpaceMetaValue = 1024 * 1024

	spaceMetaPolicyKey  = "policy"
	spaceMetaWriterKey  = "writer/"
	spaceMetaMemberType = "com.atproto.memberspace.member"
	spaceMetaMemberKey  = spaceMetaMemberType + "/"
)

// ErrInvalidSpaceMeta is returned by [ReadSpaceMetaStar] for an archive that is
// malformed or fails verification. importSpaceMeta answers it as InvalidSpaceMeta.
var ErrInvalidSpaceMeta = errors.New("invalid space meta archive")

// MetaWriter is a repo in a space's writer set, as an archive carries it: the
// repo's revision and commit data, and the space revision it last wrote at.
type MetaWriter struct {
	DID      syntax.DID
	RepoRev  syntax.TID
	Data     []byte
	SpaceRev syntax.TID
}

// SpaceMeta is a space authority's host-side state for one space, carried
// between hosts when its authority migrates. Members is only set for a
// memberspace policy, whose member list is the one piece of state a policy
// keeps beyond the policy object.
type SpaceMeta struct {
	Space   habitat_syntax.SpaceURI
	Policy  Policy
	Members []syntax.DID
	Writers []MetaWriter
}

// didToRecordKey writes a DID as a record key. A record key cannot hold the
// '%' of a localhost did:web's percent-encoded port, so that is left as ':'.
func didToRecordKey(did syntax.DID) string {
	return strings.ReplaceAll(did.String(), "%3A", ":")
}

var localhostPortKey = regexp.MustCompile(`^(did:web:[^:]+):(\d+)$`)

func recordKeyToDID(rkey string) string {
	return localhostPortKey.ReplaceAllString(rkey, "$1%3A$2")
}

// WriteSpaceMetaStar serializes a space's state per
// com.atproto.space.exportSpaceMeta: the policy, then the policy's state (the
// members, keyed as their published records would be), then one entry per
// writer, each section in strict bytewise key order.
func WriteSpaceMetaStar(meta SpaceMeta) ([]byte, error) {
	policy, err := policyMap(meta.Policy)
	if err != nil {
		return nil, err
	}
	type pair struct {
		key   string
		value map[string]any
	}
	var members, writers []pair
	for _, did := range meta.Members {
		members = append(members, pair{
			key: spaceMetaMemberKey + didToRecordKey(did),
			value: map[string]any{
				"$type": spaceMetaMemberType, "read": true, "write": true,
			},
		})
	}
	for _, w := range meta.Writers {
		writers = append(writers, pair{
			key: spaceMetaWriterKey + w.DID.String(),
			value: map[string]any{
				"repoRev":  w.RepoRev.String(),
				"data":     atdata.Bytes(w.Data),
				"spaceRev": w.SpaceRev.String(),
			},
		})
	}
	byKey := func(a, b pair) int { return strings.Compare(a.key, b.key) }
	slices.SortFunc(members, byKey)
	slices.SortFunc(writers, byKey)

	var buf bytes.Buffer
	sw, err := star.NewWriter(&buf, map[string]any{
		"$type": SpaceMetaStarType,
		"ver":   int64(spaceMetaVersion),
		"space": meta.Space.String(),
	})
	if err != nil {
		return nil, err
	}
	all := append([]pair{{spaceMetaPolicyKey, policy}}, members...)
	all = append(all, writers...)
	for _, p := range all {
		value, err := atdata.MarshalCBOR(p.value)
		if err != nil {
			return nil, fmt.Errorf("encode %s: %w", p.key, err)
		}
		if err := sw.WritePair([]byte(p.key), value); err != nil {
			return nil, err
		}
	}
	if err := sw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// policyMap is a policy as a DRISL-encodable object.
func policyMap(p Policy) (map[string]any, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return atdata.UnmarshalJSON(raw)
}

// ReadSpaceMetaStar reads and verifies a space meta archive. It is read whole,
// since it holds a space's configuration and writer set rather than its
// records. The sections must come in order (policy, policy state, writers),
// each in strict key order; a policy-state entry the policy does not define is
// refused rather than dropped, since dropping it would quietly change who has
// access to the space. Every error wraps [ErrInvalidSpaceMeta], except an
// unsupported policy, which wraps [ErrUnsupportedPolicy].
func ReadSpaceMetaStar(r io.Reader) (*SpaceMeta, error) {
	sr, err := star.NewReader(r, maxSpaceMetaValue)
	if err != nil {
		return nil, invalidMeta("%w", err)
	}
	if t := sr.Meta["$type"]; t != SpaceMetaStarType {
		return nil, invalidMeta("unexpected star profile %v", t)
	}
	if ver, ok := sr.Meta["ver"].(int64); !ok || ver != spaceMetaVersion {
		return nil, invalidMeta("unsupported version %v", sr.Meta["ver"])
	}
	space, err := habitat_syntax.ParseSpaceURI(stringField(sr.Meta, "space"))
	if err != nil {
		return nil, invalidMeta("space: %w", err)
	}

	meta := &SpaceMeta{Space: space}
	var (
		policySeen bool
		section    int // 0 policy, 1 policy state, 2 writers
		prev       []byte
	)
	for {
		key, value, err := sr.Next()
		if errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, invalidMeta("%w", err)
		}
		k := string(key)
		keySection := 1
		switch {
		case k == spaceMetaPolicyKey:
			keySection = 0
		case strings.HasPrefix(k, spaceMetaWriterKey):
			keySection = 2
		}
		if keySection < section {
			return nil, invalidMeta("section out of order at %s", k)
		}
		if keySection != section {
			section, prev = keySection, nil
		}
		if prev != nil && bytes.Compare(key, prev) <= 0 {
			return nil, invalidMeta("duplicate or out-of-order key: %s", k)
		}
		prev = key

		entry, err := atdata.UnmarshalCBOR(value)
		if err != nil {
			return nil, invalidMeta("value at %s: %w", k, err)
		}
		switch keySection {
		case 0:
			policy, err := policyFromMap(entry)
			if err != nil {
				return nil, err
			}
			meta.Policy, policySeen = policy, true
		case 1:
			did, err := memberFromEntry(meta.Policy, k, entry)
			if err != nil {
				return nil, err
			}
			meta.Members = append(meta.Members, did)
		case 2:
			writer, err := writerFromEntry(k, entry)
			if err != nil {
				return nil, err
			}
			meta.Writers = append(meta.Writers, writer)
		}
	}
	if !policySeen {
		return nil, invalidMeta("missing policy")
	}
	return meta, nil
}

func invalidMeta(format string, args ...any) error {
	return fmt.Errorf("%w: %w", ErrInvalidSpaceMeta, fmt.Errorf(format, args...))
}

func policyFromMap(entry map[string]any) (Policy, error) {
	raw, err := json.Marshal(entry)
	if err != nil {
		return Policy{}, invalidMeta("policy: %w", err)
	}
	policy, err := ParsePolicy(raw)
	if errors.Is(err, ErrInvalidPolicy) {
		return Policy{}, invalidMeta("%w", err)
	}
	// An unsupported policy is reported as such, for importSpaceMeta to answer
	// UnsupportedPolicy.
	return policy, err
}

func memberFromEntry(policy Policy, key string, entry map[string]any) (syntax.DID, error) {
	rkey, ok := strings.CutPrefix(key, spaceMetaMemberKey)
	if !ok || policy.Type != PolicyMemberSpace {
		return "", invalidMeta("unrecognized policy state: %s", key)
	}
	did, err := syntax.ParseDID(recordKeyToDID(rkey))
	if err != nil {
		return "", invalidMeta("member key %s: %w", key, err)
	}
	read, readOK := entry["read"].(bool)
	write, writeOK := entry["write"].(bool)
	if !readOK || !writeOK {
		return "", invalidMeta("invalid member at %s", key)
	}
	// Membership here is one flag granting read and write together, so a
	// member with less cannot be represented. Refuse rather than widen it.
	if !read || !write {
		return "", invalidMeta("member at %s must have both read and write access", key)
	}
	return did, nil
}

func writerFromEntry(key string, entry map[string]any) (MetaWriter, error) {
	did, err := syntax.ParseDID(strings.TrimPrefix(key, spaceMetaWriterKey))
	if err != nil {
		return MetaWriter{}, invalidMeta("writer key %s: %w", key, err)
	}
	repoRev, err := syntax.ParseTID(stringField(entry, "repoRev"))
	if err != nil {
		return MetaWriter{}, invalidMeta("repoRev at %s: %w", key, err)
	}
	spaceRev, err := syntax.ParseTID(stringField(entry, "spaceRev"))
	if err != nil {
		return MetaWriter{}, invalidMeta("spaceRev at %s: %w", key, err)
	}
	data, ok := entry["data"].(atdata.Bytes)
	if !ok || len(data) == 0 {
		return MetaWriter{}, invalidMeta("data at %s is missing", key)
	}
	return MetaWriter{DID: did, RepoRev: repoRev, Data: data, SpaceRev: spaceRev}, nil
}
