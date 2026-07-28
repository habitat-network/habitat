package utils

// PollNotifier is a coalescing "please repoll" signal between producers and a
// single listener. It exists for the common pattern of a goroutine that polls
// some source of truth (a DB table, a buffer, etc.) and wants to be woken up
// promptly when there's new work, without polling on a tight loop and without
// queueing up a backlog of redundant wakeups: at most one notification is
// ever pending, so N calls to Notify before the listener drains Listen still
// result in exactly one wakeup, after which the listener is expected to
// re-check the underlying source itself (the notification carries no
// payload).
//
// PollNotifier is not a broadcast/fan-out primitive: Listen returns the same
// channel every time, so only one goroutine should be draining it at a time.
// To notify multiple independent listeners, create one PollNotifier per
// listener and call Notify on each.
//
// Typical usage:
//
//	notif := utils.NewPollNotifier()
//
//	// listener
//	go func() {
//		ch := notif.Listen()
//		for {
//			select {
//			case <-ctx.Done():
//				return
//			case <-ch:
//				repoll()
//			}
//		}
//	}()
//
//	// producer(s)
//	notif.Notify()
type PollNotifier struct {
	ch chan struct{}
}

// NewPollNotifier returns a ready-to-use PollNotifier.
func NewPollNotifier() *PollNotifier {
	return &PollNotifier{ch: make(chan struct{}, 1)}
}

// Listen returns the channel the listener should select on. It always
// returns the same channel, so only one consumer should drain it at a time.
func (n *PollNotifier) Listen() <-chan struct{} {
	return n.ch
}

// Notify wakes the listener so it repolls. It never blocks: if a
// notification is already pending (the listener hasn't drained Listen yet),
// the call is a no-op rather than queuing a second wakeup.
func (n *PollNotifier) Notify() {
	select {
	case n.ch <- struct{}{}:
	default:
	}
}
