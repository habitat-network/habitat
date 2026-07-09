package spaces

import (
	"crypto/sha256"
	"encoding/binary"

	"lukechampine.com/blake3"
)

// ltHashLanes is the number of 16-bit lanes in an LtHash state. The state is a
// fixed 2048-byte buffer interpreted as 1024 little-endian uint16 lanes.
const ltHashLanes = 1024

// ltHashStateBytes is the serialized LtHash state size in bytes.
const ltHashStateBytes = 2 * ltHashLanes

// ltHash is a homomorphic set hash over a permissioned repo's records, per the
// atproto permissioned-data proposal. Each record folds in as a single lane-wise
// addition (mod 2^16), so records can be added or removed incrementally without
// recomputing the whole digest. The empty repo's state is all zeroes.
type ltHash struct {
	lanes [ltHashLanes]uint16
}

// recordElement is the LtHash element for a record: the UTF-8 bytes of
// "{collection}/{rkey}/{cid}".
func recordElement(collection, rkey, recordCID string) string {
	return collection + "/" + rkey + "/" + recordCID
}

// add folds an element into the state.
func (h *ltHash) add(element string) { h.fold(element, false) }

// remove folds an element out of the state.
func (h *ltHash) remove(element string) { h.fold(element, true) }

func (h *ltHash) fold(element string, subtract bool) {
	// Expand the element to 2048 bytes with BLAKE3 in XOF mode, then read those
	// bytes as 1024 little-endian uint16 lanes.
	x := blake3.New(ltHashStateBytes, nil)
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

// loadLtHash reconstructs an ltHash from its serialized 2048-byte state. A state
// of the wrong length (e.g. an empty/missing row) yields the zero (empty) hash.
func loadLtHash(state []byte) ltHash {
	var h ltHash
	if len(state) != ltHashStateBytes {
		return h
	}
	for i := range ltHashLanes {
		h.lanes[i] = binary.LittleEndian.Uint16(state[i*2:])
	}
	return h
}

// state returns the 2048-byte little-endian serialization of the lanes.
func (h *ltHash) state() []byte {
	b := make([]byte, ltHashStateBytes)
	for i, lane := range h.lanes {
		binary.LittleEndian.PutUint16(b[i*2:], lane)
	}
	return b
}

// sum returns sha256(state), the 32-byte commit hash.
func (h *ltHash) sum() []byte {
	s := sha256.Sum256(h.state())
	return s[:]
}
