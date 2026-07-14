package spaces

import (
	"fmt"
	"io"
	"sort"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	"github.com/ipld/go-car"
	util "github.com/ipld/go-car/util"

	"github.com/habitat-network/habitat/internal/spacecommit"
)

type recordBlock struct {
	Collection syntax.NSID
	Rkey       syntax.RecordKey
	Cid        cid.Cid
	Bytes      []byte
}

func recordPath(collection syntax.NSID, rkey syntax.RecordKey) string {
	return collection.String() + "/" + rkey.String()
}

// signedCommitBlock encodes a signed commit as the DAG-CBOR block that forms the
// CAR's first root, matching network.habitat.space.defs#signedCommit.
func signedCommitBlock(c spacecommit.SignedCommit) ([]byte, cid.Cid, error) {
	commit := map[string]any{
		"ver":  int64(c.Ver),
		"hash": atdata.Bytes(c.Hash),
		"ikm":  atdata.Bytes(c.Ikm),
		"sig":  atdata.Bytes(c.Sig),
		"mac":  atdata.Bytes(c.Mac),
		"rev":  c.Rev,
	}
	bytes, err := atdata.MarshalCBOR(commit)
	if err != nil {
		return nil, cid.Cid{}, fmt.Errorf("marshal signed commit: %w", err)
	}
	commitCID, err := cid.V1Builder{}.WithCodec(cid.DagCBOR).Sum(bytes)
	if err != nil {
		return nil, cid.Cid{}, fmt.Errorf("compute signed commit cid: %w", err)
	}
	return bytes, commitCID, nil
}

func buildIndexBlock(blocks []recordBlock) ([]byte, cid.Cid, error) {
	index := make(map[string]any, len(blocks))
	for _, block := range blocks {
		index[recordPath(block.Collection, block.Rkey)] = atdata.CIDLink(block.Cid)
	}
	bytes, err := atdata.MarshalCBOR(index)
	if err != nil {
		return nil, cid.Cid{}, fmt.Errorf("marshal repo index: %w", err)
	}
	indexCID, err := cid.V1Builder{}.WithCodec(cid.DagCBOR).Sum(bytes)
	if err != nil {
		return nil, cid.Cid{}, fmt.Errorf("compute repo index cid: %w", err)
	}
	return bytes, indexCID, nil
}

func writeRepoCAR(
	w io.Writer,
	commitCID cid.Cid,
	commitBytes []byte,
	indexCID cid.Cid,
	indexBytes []byte,
	blocks []recordBlock,
) error {
	header := car.CarHeader{
		Version: 1,
		Roots:   []cid.Cid{commitCID, indexCID},
	}
	if err := car.WriteHeader(&header, w); err != nil {
		return fmt.Errorf("write car header: %w", err)
	}
	if err := writeCARBlock(w, commitCID, commitBytes); err != nil {
		return err
	}
	if err := writeCARBlock(w, indexCID, indexBytes); err != nil {
		return err
	}
	for _, block := range blocks {
		if err := writeCARBlock(w, block.Cid, block.Bytes); err != nil {
			return err
		}
	}
	return nil
}

func writeCARBlock(w io.Writer, blockCID cid.Cid, data []byte) error {
	if err := util.LdWrite(w, blockCID.Bytes(), data); err != nil {
		return fmt.Errorf("write car block %s: %w", blockCID, err)
	}
	return nil
}

// SerializeRepoCAR serializes a permissioned repo to CAR per
// com.atproto.space.getRepo. commit is the repo's signed head commit, and blocks
// are its current records. The CAR declares two roots in order — the signed
// commit, then the DRISL index — followed by the record blocks in lexicographic
// order by "{collection}/{rkey}".
func SerializeRepoCAR(
	commit spacecommit.SignedCommit,
	blocks []recordBlock,
) ([]byte, error) {
	sorted := append([]recordBlock(nil), blocks...)
	sort.Slice(sorted, func(i, j int) bool {
		return recordPath(sorted[i].Collection, sorted[i].Rkey) <
			recordPath(sorted[j].Collection, sorted[j].Rkey)
	})

	commitBytes, commitCID, err := signedCommitBlock(commit)
	if err != nil {
		return nil, err
	}
	indexBytes, indexCID, err := buildIndexBlock(sorted)
	if err != nil {
		return nil, err
	}

	var buf []byte
	writer := &bytesWriter{buf: &buf}
	if err := writeRepoCAR(
		writer,
		commitCID,
		commitBytes,
		indexCID,
		indexBytes,
		sorted,
	); err != nil {
		return nil, err
	}
	return buf, nil
}

type bytesWriter struct {
	buf *[]byte
}

func (w *bytesWriter) Write(p []byte) (int, error) {
	*w.buf = append(*w.buf, p...)
	return len(p), nil
}
