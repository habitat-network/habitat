package mcpserver

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

const (
	// contextPagesTTL is how long get_context keeps a ranked result set for
	// its next_cursor to page through.
	contextPagesTTL = 10 * time.Minute
	// contextPagesMax bounds how many ranked result sets are kept at once.
	contextPagesMax = 1000
)

// contextSnapshot is one get_context search's complete ranked results. The
// pages after the first are slices of it, so they stay consistent with the
// ordering the first page was cut from, however the sources change since.
type contextSnapshot struct {
	caller  syntax.DID
	records []contextRecord
	items   []contextItem
	expires time.Time
}

// contextPages keeps contextSnapshots, in memory, for a limited time. A
// cursor that outlives its snapshot (expired, evicted, or lost to a restart
// or another replica) is invalid and the client searches again.
type contextPages struct {
	mu    sync.Mutex
	ttl   time.Duration
	now   func() time.Time
	snaps map[string]*contextSnapshot
}

func newContextPages() *contextPages {
	return &contextPages{ttl: contextPagesTTL, now: time.Now, snaps: map[string]*contextSnapshot{}}
}

// contextCursor is the decoded form of get_context's opaque cursor: which
// snapshot, and how far into its records and items the previous page got.
type contextCursor struct {
	ID      string `json:"id"`
	Records int    `json:"r"`
	Items   int    `json:"i"`
}

func (c contextCursor) encode() string {
	data, _ := json.Marshal(c) // a struct of strings and ints can't fail
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeContextCursor(s string) (contextCursor, bool) {
	var c contextCursor
	data, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || json.Unmarshal(data, &c) != nil {
		return c, false
	}
	return c, c.ID != "" && c.Records >= 0 && c.Items >= 0
}

// put stores snap and returns the id a cursor refers to it by.
func (p *contextPages) put(snap *contextSnapshot) string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	id := base64.RawURLEncoding.EncodeToString(buf)

	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	snap.expires = now.Add(p.ttl)
	for k, s := range p.snaps {
		if !s.expires.After(now) {
			delete(p.snaps, k)
		}
	}
	// Still full of live snapshots: drop the one closest to expiry.
	for len(p.snaps) >= contextPagesMax {
		var oldest string
		for k, s := range p.snaps {
			if oldest == "" || s.expires.Before(p.snaps[oldest].expires) {
				oldest = k
			}
		}
		delete(p.snaps, oldest)
	}
	p.snaps[id] = snap
	return id
}

// get returns the snapshot id names, if it's still held and caller made it.
// Every cursor use extends the snapshot's life, so a client paging steadily
// never has it expire under them.
func (p *contextPages) get(id string, caller syntax.DID) (*contextSnapshot, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	snap, ok := p.snaps[id]
	if !ok || !snap.expires.After(p.now()) || snap.caller != caller {
		return nil, false
	}
	snap.expires = p.now().Add(p.ttl)
	return snap, true
}
