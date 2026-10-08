package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/ipfs/go-cid"
	"golang.org/x/net/html"

	"github.com/habitat-network/habitat/internal/spaces"
)

// BlobSource is the part of [spaces.BlobStore] the [Indexer] reads blobs from.
type BlobSource interface {
	GetBlob(ctx context.Context, c cid.Cid) (mimeType string, data []byte, err error)
}

const (
	// maxBlobBytes is the largest blob whose content is indexed; larger blobs
	// are skipped.
	maxBlobBytes = 5 << 20
	// maxBlobTextBytes caps the blob text added to one record's document, over
	// all of its blobs.
	maxBlobTextBytes = 256 << 10
)

// blobExtractors maps the mime types whose blob content is indexed to the
// function turning their bytes into text. The record's blob refs are matched
// against these by their media type alone, ignoring parameters like charset.
var blobExtractors = map[string]func(context.Context, []byte) string{
	"text/plain":       textFromPlain,
	"text/markdown":    textFromPlain,
	"text/html":        textFromHTML,
	"application/json": textFromJSON,
}

// blobTypeSupported reports whether the content of blobs of mimeType is
// indexed.
func blobTypeSupported(mimeType string) bool {
	_, ok := blobExtractors[mediaType(mimeType)]
	return ok
}

func mediaType(mimeType string) string {
	mt, _, err := mime.ParseMediaType(mimeType)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(mimeType))
	}
	return mt
}

// WithBlobs makes the indexer also index the content of the blobs a record
// references, when their mime type is one of the supported text types, as
// part of the record's document. The text of a blob is therefore readable and
// filterable exactly like its record.
func WithBlobs(src BlobSource) IndexerOption {
	return func(x *Indexer) { x.blobs = src }
}

// blobText returns the text of the supported blobs record references, joined
// by spaces. Blobs that are missing, too large, unsupported or unreadable are
// skipped, so one bad blob never stops its record from being indexed.
func (x *Indexer) blobText(ctx context.Context, record map[string]any) string {
	if x.blobs == nil {
		return ""
	}
	var sb strings.Builder
	seen := map[string]bool{}
	for _, ref := range atdata.ExtractBlobs(record) {
		c := cid.Cid(ref.Ref)
		if sb.Len() >= maxBlobTextBytes || seen[c.String()] ||
			ref.Size > maxBlobBytes || !blobTypeSupported(ref.MimeType) {
			continue
		}
		seen[c.String()] = true
		mimeType, data, err := x.blobs.GetBlob(ctx, c)
		if errors.Is(err, spaces.ErrBlobNotFound) {
			continue
		}
		if err != nil {
			slog.WarnContext(ctx, "search: read blob failed", "cid", c.String(), "err", err)
			continue
		}
		// The stored type and size are authoritative, not the record's claim.
		extract, ok := blobExtractors[mediaType(mimeType)]
		if !ok || len(data) > maxBlobBytes {
			continue
		}
		if text := clean(extract(ctx, data)); text != "" {
			sb.WriteString(text)
			sb.WriteByte(' ')
		}
	}
	return truncateUTF8(strings.TrimSpace(sb.String()), maxBlobTextBytes)
}

func textFromPlain(_ context.Context, data []byte) string { return string(data) }

// textFromHTML returns the text nodes of an HTML document, skipping scripts
// and styles.
func textFromHTML(ctx context.Context, data []byte) string {
	var sb strings.Builder
	z := html.NewTokenizer(bytes.NewReader(data))
	skip := 0
	for {
		switch z.Next() {
		case html.ErrorToken:
			if !errors.Is(z.Err(), io.EOF) {
				slog.DebugContext(ctx, "search: html blob parse stopped", "err", z.Err())
			}
			return sb.String()
		case html.StartTagToken:
			if name, _ := z.TagName(); isHiddenTag(name) {
				skip++
			}
		case html.EndTagToken:
			if name, _ := z.TagName(); isHiddenTag(name) && skip > 0 {
				skip--
			}
		case html.TextToken:
			if skip == 0 {
				sb.Write(z.Text())
				sb.WriteByte(' ')
			}
		}
	}
}

func isHiddenTag(name []byte) bool {
	s := string(name)
	return s == "script" || s == "style"
}

// textFromJSON returns the prose strings of a JSON document, as [ExtractText]
// does for a record. Invalid JSON yields no text.
func textFromJSON(_ context.Context, data []byte) string {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return ""
	}
	var sb strings.Builder
	walk(v, &sb)
	return sb.String()
}
