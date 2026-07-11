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

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

type recordBlock struct {
	Collection syntax.NSID
	Rkey       syntax.RecordKey
	Cid        cid.Cid
	Bytes      []byte
	Rev        syntax.TID
}

func recordPath(collection syntax.NSID, rkey syntax.RecordKey) string {
	return collection.String() + "/" + rkey.String()
}

func latestRev(blocks []recordBlock) string {
	var latest syntax.TID
	for _, block := range blocks {
		if block.Rev > latest {
			latest = block.Rev
		}
	}
	return string(latest)
}

// placeholderSignedCommitBlock returns a DAG-CBOR block for a signed commit.
// Real signing and LtHash are not implemented yet.
func placeholderSignedCommitBlock(rev string) ([]byte, cid.Cid, error) {
	commit := map[string]any{
		"ver":  int64(1),
		"hash": atdata.Bytes(make([]byte, 32)),
		"ikm":  atdata.Bytes(make([]byte, 32)),
		"sig":  atdata.Bytes([]byte("placeholder")),
		"mac":  atdata.Bytes(make([]byte, 32)),
		"rev":  rev,
	}
	bytes, err := atdata.MarshalCBOR(commit)
	if err != nil {
		return nil, cid.Cid{}, fmt.Errorf("marshal placeholder signed commit: %w", err)
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

// SerializeRepoCAR serializes a permissioned repo to CAR per com.atproto.space.getRepo.
func SerializeRepoCAR(
	_ habitat_syntax.SpaceURI,
	_ syntax.DID,
	blocks []recordBlock,
) ([]byte, error) {
	sorted := append([]recordBlock(nil), blocks...)
	sort.Slice(sorted, func(i, j int) bool {
		return recordPath(sorted[i].Collection, sorted[i].Rkey) <
			recordPath(sorted[j].Collection, sorted[j].Rkey)
	})

	commitBytes, commitCID, err := placeholderSignedCommitBlock(latestRev(sorted))
	if err != nil {
		return nil, err
	}
	indexBytes, indexCID, err := buildIndexBlock(sorted)
	if err != nil {
		return nil, err
	}

	var buf []byte
	writer := &bytesWriter{buf: &buf}
	if err := writeRepoCAR(writer, commitCID, commitBytes, indexCID, indexBytes, sorted); err != nil {
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
