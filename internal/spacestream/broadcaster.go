package spacestream

import (
	"context"
	"sync"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Broadcaster wakes every stream waiting at the head of the log when the
// spaces store reports a write. It is a [spaces.Notifier], so pear notifies it
// alongside registered syncers. A wake carries nothing: woken streams read the
// log themselves, so there is no per-stream queue to fill up.
type Broadcaster struct {
	mu sync.Mutex
	// ch is closed, and replaced, on the next notification.
	ch chan struct{}
}

var _ spaces.Notifier = (*Broadcaster)(nil)

// NewBroadcaster returns a Broadcaster with no waiters.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{ch: make(chan struct{})}
}

// Wait returns a channel that is closed on the next notification. Take it
// before reading the log, so a write that lands during the read still wakes
// the caller afterwards.
func (b *Broadcaster) Wait() <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ch
}

func (b *Broadcaster) wake() {
	b.mu.Lock()
	defer b.mu.Unlock()
	close(b.ch)
	b.ch = make(chan struct{})
}

// NotifyWrite implements [spaces.Notifier].
func (b *Broadcaster) NotifyWrite(
	context.Context,
	habitat_syntax.SpaceURI,
	syntax.DID,
	syntax.TID,
	[]byte,
	syntax.TID,
	syntax.TID,
) {
	b.wake()
}

// NotifySpaceDeleted implements [spaces.Notifier].
func (b *Broadcaster) NotifySpaceDeleted(context.Context, habitat_syntax.SpaceURI) {
	b.wake()
}
