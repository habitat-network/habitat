package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
)

var errSign = errors.New("sign failed")

// fakeSigner records the service-auth requests it is asked to sign and returns a
// fixed token, or err when set.
type fakeSigner struct {
	t   *testing.T
	err error
}

func (f *fakeSigner) PrivateKeyForDID(
	_ context.Context,
	_ syntax.DID,
) (atcrypto.PrivateKey, error) {
	if f.err != nil {
		return nil, f.err
	}
	return atcrypto.GeneratePrivateKeyK256()
}

func TestNotifierDeliversToRegisteredEndpoints(t *testing.T) {
	s := newTestStore(t)

	type delivery struct {
		in habitat.NetworkHabitatSpaceNotifyWriteInput
	}
	received := make(chan delivery, 2)
	handler := func(w http.ResponseWriter, r *http.Request) {
		var in habitat.NetworkHabitatSpaceNotifyWriteInput
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		require.Contains(t, r.Header.Get("Authorization"), "Bearer ")
		received <- delivery{in: in}
		w.WriteHeader(http.StatusOK)
	}
	subscriber := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(subscriber.Close)

	future := time.Now().Add(time.Hour)
	// One whole-space and one repo-specific registration both match this write.
	require.NoError(t, s.Register(t.Context(), space, "", subscriber.URL, future))
	require.NoError(t, s.Register(t.Context(), space, repo, subscriber.URL, future))

	signer := &fakeSigner{t: t}
	notifier := NewNotifier(s, subscriber.Client(), signer)
	notifier.NotifyWrite(t.Context(), space, repo, "3lrev")

	for range 2 {
		select {
		case d := <-received:
			require.Equal(t, space.String(), d.in.Space)
			require.Equal(t, repo.String(), d.in.Repo)
			require.Equal(t, "3lrev", d.in.Rev)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for notifyWrite delivery")
		}
	}
}

func TestNotifierNotifySpaceDeleted(t *testing.T) {
	s := newTestStore(t)

	received := make(chan habitat.NetworkHabitatSpaceNotifySpaceDeletedInput, 2)
	subscriber := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in habitat.NetworkHabitatSpaceNotifySpaceDeletedInput
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		received <- in
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(subscriber.Close)

	future := time.Now().Add(time.Hour)
	// Both a whole-space and a repo-specific registration should be notified.
	require.NoError(t, s.Register(t.Context(), space, "", subscriber.URL, future))
	require.NoError(t, s.Register(t.Context(), space, repo, subscriber.URL, future))

	signer := &fakeSigner{t: t}
	notifier := NewNotifier(s, subscriber.Client(), signer)
	notifier.NotifySpaceDeleted(t.Context(), space)

	for range 2 {
		select {
		case in := <-received:
			require.Equal(t, space.String(), in.Space)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for notifySpaceDeleted delivery")
		}
	}
}

func TestNotifierNoRegistrations(t *testing.T) {
	s := newTestStore(t)
	signer := &fakeSigner{t: t}
	notifier := NewNotifier(s, http.DefaultClient, signer)

	// With no registrations, neither path should sign or deliver anything.
	notifier.NotifyWrite(t.Context(), space, repo, "3lrev")
	notifier.NotifySpaceDeleted(t.Context(), space)
}

func TestNotifierSignerErrorAbortsDelivery(t *testing.T) {
	s := newTestStore(t)

	delivered := make(chan struct{}, 1)
	subscriber := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		delivered <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(subscriber.Close)

	future := time.Now().Add(time.Hour)
	require.NoError(t, s.Register(t.Context(), space, "", subscriber.URL, future))

	signer := &fakeSigner{err: errSign}
	notifier := NewNotifier(s, subscriber.Client(), signer)
	notifier.NotifyWrite(t.Context(), space, repo, "3lrev")

	select {
	case <-delivered:
		t.Fatal("delivered despite service-auth signing failure")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestNotifierSkipsUnmatchedRepo(t *testing.T) {
	s := newTestStore(t)

	delivered := make(chan struct{}, 1)
	subscriber := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		delivered <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(subscriber.Close)

	// Registration targets bob, but the write is for repo (alice).
	require.NoError(
		t,
		s.Register(t.Context(), space, bob, subscriber.URL, time.Now().Add(time.Hour)),
	)

	notifier := NewNotifier(s, subscriber.Client(), &fakeSigner{t: t})
	notifier.NotifyWrite(t.Context(), space, repo, "3lrev")

	select {
	case <-delivered:
		t.Fatal("delivered notifyWrite to a non-matching registration")
	case <-time.After(200 * time.Millisecond):
	}
}
