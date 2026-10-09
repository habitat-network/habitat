package pearserver

import (
	"bytes"
	"mime"
	"net/http"
	"regexp"
	"strconv"
)

// spaceDefType matches a `$type` naming a def (`<nsid>#<name>`) of a
// network.habitat.space lexicon. Record types have no `#`, so a record of a
// network.habitat.space type (e.g. appAccess) is left alone.
var spaceDefType = regexp.MustCompile(`("\$type"\s*:\s*")network\.habitat\.space\.([A-Za-z]+#)`)

// withComAtprotoSpaceTypes serves h under its com.atproto.space alias: the
// handlers encode network.habitat.space types, so `$type`s naming one of its
// defs (e.g. listRepos#repo, defs#signedCommit) are renamed to the matching
// com.atproto.space def, which a com.atproto.space client validates against.
// Non-JSON responses (CARs, blobs) pass through untouched.
func withComAtprotoSpaceTypes(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rw := &typeRewriter{ResponseWriter: w, status: http.StatusOK}
		h(rw, r)
		rw.flush()
	}
}

// typeRewriter buffers a JSON response so its `$type`s can be renamed, and
// streams anything else straight through.
type typeRewriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	passthrough bool
	buf         bytes.Buffer
}

func (t *typeRewriter) WriteHeader(status int) {
	if t.wroteHeader {
		return
	}
	t.wroteHeader = true
	t.status = status
	mediaType, _, _ := mime.ParseMediaType(t.Header().Get("Content-Type"))
	t.passthrough = mediaType != "application/json"
	if t.passthrough {
		t.ResponseWriter.WriteHeader(status)
	}
}

func (t *typeRewriter) Write(b []byte) (int, error) {
	if !t.wroteHeader {
		t.WriteHeader(http.StatusOK)
	}
	if t.passthrough {
		return t.ResponseWriter.Write(b)
	}
	return t.buf.Write(b)
}

// Flush lets a streaming handler flush through when the response passes
// through unbuffered.
func (t *typeRewriter) Flush() {
	if f, ok := t.ResponseWriter.(http.Flusher); ok && t.passthrough {
		f.Flush()
	}
}

func (t *typeRewriter) flush() {
	if !t.wroteHeader || t.passthrough {
		return
	}
	body := spaceDefType.ReplaceAll(t.buf.Bytes(), []byte("${1}com.atproto.space.${2}"))
	t.Header().Set("Content-Length", strconv.Itoa(len(body)))
	t.ResponseWriter.WriteHeader(t.status)
	_, _ = t.ResponseWriter.Write(body)
}
