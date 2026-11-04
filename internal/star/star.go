// Package star reads and writes STAR-lite v1 archives: a metadata header
// followed by key/value pairs until EOF.
//
//	[ magic | len | meta ] [ len | key | len | val ] [ len | key | len | val ] ...
//
// Each len is an unsigned varint in its shortest form, and meta is a DRISL
// (canonical DAG-CBOR) map whose "$type" names the archive's profile, which
// defines what the keys and values mean. This mirrors @atproto/space's star.ts.
//
// See https://tangled.org/microcosm.blue/star/tree/main/v1-star
package star

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/bluesky-social/indigo/atproto/atdata"
)

// Magic is the four-byte prefix of every STAR-lite v1 archive.
var Magic = []byte{0x2a, 0x6c, 0x01, 0x00}

const (
	// maxVarintBytes bounds a length prefix, as in the reference implementation.
	maxVarintBytes = 6
	// MaxMetaLength is the largest metadata frame a reader accepts.
	MaxMetaLength = 64 * 1024 * 1024
	// MaxKeyLength is the largest key frame a reader accepts.
	MaxKeyLength = 64 * 1024 * 1024
	// MaxValueLength is STAR's own value limit; a profile may set a lower one.
	MaxValueLength = 64 * 1024 * 1024 * 1024
)

// ErrParse is wrapped by every error a Reader returns for a malformed archive.
var ErrParse = errors.New("star parse error")

func parseErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrParse, fmt.Sprintf(format, args...))
}

// Writer writes a STAR archive. The metadata is written by NewWriter; pairs
// follow with WritePair, in whatever order the profile requires.
type Writer struct {
	w io.Writer
}

// NewWriter writes the magic and the DRISL encoding of meta, which must carry
// a string "$type".
func NewWriter(w io.Writer, meta map[string]any) (*Writer, error) {
	if _, ok := meta["$type"].(string); !ok {
		return nil, errors.New("star metadata must have a string $type")
	}
	metaBytes, err := atdata.MarshalCBOR(meta)
	if err != nil {
		return nil, fmt.Errorf("encode star metadata: %w", err)
	}
	sw := &Writer{w: w}
	if _, err := w.Write(Magic); err != nil {
		return nil, fmt.Errorf("write star magic: %w", err)
	}
	if err := sw.frame(metaBytes); err != nil {
		return nil, fmt.Errorf("write star metadata: %w", err)
	}
	return sw, nil
}

// WritePair writes one key/value pair.
func (sw *Writer) WritePair(key, value []byte) error {
	if err := sw.frame(key); err != nil {
		return fmt.Errorf("write star key: %w", err)
	}
	if err := sw.frame(value); err != nil {
		return fmt.Errorf("write star value: %w", err)
	}
	return nil
}

func (sw *Writer) frame(b []byte) error {
	var lenBuf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(lenBuf[:], uint64(len(b)))
	if _, err := sw.w.Write(lenBuf[:n]); err != nil {
		return err
	}
	_, err := sw.w.Write(b)
	return err
}

// Reader reads a STAR archive. NewReader reads the header eagerly; pairs are
// read one at a time with Next.
type Reader struct {
	r              *bufio.Reader
	maxValueLength uint64
	// Meta is the archive's decoded metadata.
	Meta map[string]any
}

// NewReader reads and validates the magic and metadata of a STAR archive.
// maxValueLength caps each value frame (0 means STAR's own MaxValueLength);
// lengths are checked before a frame is read, so an oversized frame is never
// buffered.
func NewReader(r io.Reader, maxValueLength uint64) (*Reader, error) {
	if maxValueLength == 0 {
		maxValueLength = MaxValueLength
	}
	sr := &Reader{r: bufio.NewReader(r), maxValueLength: maxValueLength}
	magic := make([]byte, len(Magic))
	if _, err := io.ReadFull(sr.r, magic); err != nil || !bytes.Equal(magic, Magic) {
		return nil, parseErr("invalid magic bytes")
	}
	metaBytes, err := sr.readFrame(MaxMetaLength)
	if err != nil {
		return nil, err
	}
	if metaBytes == nil {
		return nil, parseErr("missing metadata")
	}
	meta, err := atdata.UnmarshalCBOR(metaBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid metadata: %w", ErrParse, err)
	}
	if _, ok := meta["$type"].(string); !ok {
		return nil, parseErr("metadata must be a map with a $type")
	}
	// The decoder accepts some non-canonical forms (such as unsorted map
	// keys), so check that the metadata re-encodes to the same bytes. Note
	// atdata decodes an empty byte string as nil (re-encoded as null), so
	// metadata holding one is rejected; writers omit empty byte fields.
	reencoded, err := atdata.MarshalCBOR(meta)
	if err != nil || !bytes.Equal(reencoded, metaBytes) {
		return nil, parseErr("metadata is not canonical DRISL")
	}
	sr.Meta = meta
	return sr, nil
}

// Next returns the next key/value pair, or io.EOF once the archive ends at a
// pair boundary.
func (sr *Reader) Next() (key, value []byte, err error) {
	key, err = sr.readFrame(MaxKeyLength)
	if err != nil {
		return nil, nil, err
	}
	if key == nil {
		return nil, nil, io.EOF
	}
	value, err = sr.readFrame(sr.maxValueLength)
	if err != nil {
		return nil, nil, err
	}
	if value == nil {
		return nil, nil, parseErr("missing value for key")
	}
	return key, value, nil
}

// readFrame reads one length-prefixed frame. It returns nil, nil at EOF before
// a frame starts, which is only allowed at a pair boundary.
func (sr *Reader) readFrame(maxLength uint64) ([]byte, error) {
	length, ok, err := sr.readVarint()
	if err != nil || !ok {
		return nil, err
	}
	if length > maxLength {
		return nil, parseErr("length %d exceeds limit of %d", length, maxLength)
	}
	b := make([]byte, length)
	if _, err := io.ReadFull(sr.r, b); err != nil {
		return nil, parseErr("unexpected end of archive")
	}
	return b, nil
}

// readVarint reads an unsigned varint in its shortest form: 7 bits per byte,
// least significant group first, with the high bit set on every byte but the
// last. ok is false at a clean EOF before the first byte.
func (sr *Reader) readVarint() (value uint64, ok bool, err error) {
	for i := range maxVarintBytes {
		b, err := sr.r.ReadByte()
		if err != nil {
			if i == 0 && errors.Is(err, io.EOF) {
				return 0, false, nil
			}
			return 0, false, parseErr("unexpected end of archive")
		}
		value |= uint64(b&0x7f) << (7 * i)
		if b < 0x80 {
			// A final zero byte only pads the encoding, which STAR forbids.
			if b == 0 && i > 0 {
				return 0, false, parseErr("non-canonical varint")
			}
			return value, true, nil
		}
	}
	return 0, false, parseErr("varint too long")
}
