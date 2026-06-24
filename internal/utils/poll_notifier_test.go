package utils

import "testing"

func TestPollNotifier_NotifyDeliversToListen(t *testing.T) {
	n := NewPollNotifier()

	n.Notify()

	select {
	case <-n.Listen():
	default:
		t.Fatal("expected Listen channel to receive a notification")
	}
}

func TestPollNotifier_NotifyWhenPendingIsDropped(t *testing.T) {
	n := NewPollNotifier()

	n.Notify()
	n.Notify() // should not block even though a notification is already pending

	select {
	case <-n.Listen():
	default:
		t.Fatal("expected Listen channel to receive a notification")
	}

	select {
	case <-n.Listen():
		t.Fatal("expected only one notification to be pending")
	default:
	}
}

func TestPollNotifier_ListenWithoutNotifyDoesNotReceive(t *testing.T) {
	n := NewPollNotifier()

	select {
	case <-n.Listen():
		t.Fatal("expected no notification without a call to Notify")
	default:
	}
}
