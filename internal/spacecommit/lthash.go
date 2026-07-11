// Package spacecommit implements the atproto permissioned-data proposal's repo
// hashing (LtHash) and signed commits. It is shared by the space host (which
// builds commits) and syncers such as sap (which recompute the hash and verify
// commits).
package spacecommit

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"lukechampine.com/blake3"
)

// ltHashLanes is the number of 16-bit lanes in an LtHash state. The state is a
// fixed 2048-byte buffer interpreted as 1024 little-endian uint16 lanes.
const ltHashLanes = 1024

// LtHashStateBytes is the serialized LtHash state size in bytes.
const LtHashStateBytes = 2 * ltHashLanes

// LtHash is a homomorphic set hash over a permissioned repo's records, per the
// atproto permissioned-data proposal. Each record folds in as a single lane-wise
// addition (mod 2^16), so records can be added or removed incrementally without
// recomputing the whole digest. The empty repo's state is all zeroes.
type LtHash struct {
	lanes [ltHashLanes]uint16
}

// RecordElement is the LtHash element for a record: the UTF-8 bytes of
// "{collection}/{rkey}/{cid}".
func RecordElement(collection syntax.NSID, rkey syntax.RecordKey, recordCID string) string {
	return collection.String() + "/" + rkey.String() + "/" + recordCID
}

// Add folds an element into the state.
func (h *LtHash) Add(element string) { h.fold(element, false) }

// Remove folds an element out of the state.
func (h *LtHash) Remove(element string) { h.fold(element, true) }

func (h *LtHash) fold(element string, subtract bool) {
	// Expand the element to 2048 bytes with BLAKE3 in XOF mode, then read those
	// bytes as 1024 little-endian uint16 lanes.
	x := blake3.New(LtHashStateBytes, nil)
	_, _ = x.Write([]byte(element))
	expanded := x.Sum(nil)
	for i := range ltHashLanes {
		lane := binary.LittleEndian.Uint16(expanded[i*2:])
		if subtract {
			h.lanes[i] -= lane
		} else {
			h.lanes[i] += lane
		}
	}
}

// Load reconstructs an LtHash from its serialized 2048-byte state. A state of the
// wrong length (e.g. an empty/missing row) yields the zero (empty) hash.
func Load(state []byte) LtHash {
	var h LtHash
	if len(state) != LtHashStateBytes {
		return h
	}
	for i := range ltHashLanes {
		h.lanes[i] = binary.LittleEndian.Uint16(state[i*2:])
	}
	return h
}

// State returns the 2048-byte little-endian serialization of the lanes.
func (h *LtHash) State() []byte {
	b := make([]byte, LtHashStateBytes)
	for i, lane := range h.lanes {
		binary.LittleEndian.PutUint16(b[i*2:], lane)
	}
	return b
}

// Sum returns sha256(state), the 32-byte commit hash.
func (h *LtHash) Sum() []byte {
	s := sha256.Sum256(h.State())
	return s[:]
}
