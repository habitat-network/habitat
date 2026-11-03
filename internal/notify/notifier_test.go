package notify

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
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
	require.NoError(
		t,
		s.Register(
			t.Context(),
			NamespaceHabitat,
			space,
			"",
			subscriber.URL,
			subscriber.URL,
			future,
		),
	)
	require.NoError(
		t,
		s.Register(
			t.Context(),
			NamespaceHabitat,
			space,
			repo,
			subscriber.URL,
			subscriber.URL,
			future,
		),
	)

	signer := &fakeSigner{t: t}
	notifier := NewNotifier(s, subscriber.Client(), signer)
	notifier.NotifyWrite(
		t.Context(),
		space,
		repo,
		"3lrev",
		[]byte{0x01, 0x02},
		"3lspacerev2",
		"3lspacerev1",
	)

	for range 2 {
		select {
		case d := <-received:
			require.Equal(t, space.String(), d.in.Space)
			require.Equal(t, repo.String(), d.in.Repo)
			require.Equal(t, "3lrev", d.in.Rev)
			require.Equal(t, []byte{0x01, 0x02}, []byte(d.in.Hash))
			require.Equal(t, "3lspacerev2", d.in.SpaceRev)
			require.Equal(t, "3lspacerev1", d.in.PrevSpaceRev)
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
	// A syncer registered for the whole space and for one repo in it is one
	// audience, and hears of the deletion once.
	require.NoError(
		t,
		s.Register(
			t.Context(),
			NamespaceHabitat,
			space,
			"",
			subscriber.URL,
			subscriber.URL,
			future,
		),
	)
	require.NoError(
		t,
		s.Register(
			t.Context(),
			NamespaceHabitat,
			space,
			repo,
			subscriber.URL,
			subscriber.URL,
			future,
		),
	)

	signer := &fakeSigner{t: t}
	notifier := NewNotifier(s, subscriber.Client(), signer)
	notifier.NotifySpaceDeleted(t.Context(), space)

	select {
	case in := <-received:
		require.Equal(t, space.String(), in.Space)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for notifySpaceDeleted delivery")
	}
	select {
	case <-received:
		t.Fatal("notified the same syncer twice")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestNotifierNoRegistrations(t *testing.T) {
	s := newTestStore(t)
	signer := &fakeSigner{t: t}
	notifier := NewNotifier(s, http.DefaultClient, signer)

	// With no registrations, neither path should sign or deliver anything.
	notifier.NotifyWrite(
		t.Context(),
		space,
		repo,
		"3lrev",
		[]byte{0x01, 0x02},
		"3lspacerev2",
		"3lspacerev1",
	)
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
	require.NoError(
		t,
		s.Register(
			t.Context(),
			NamespaceHabitat,
			space,
			"",
			subscriber.URL,
			subscriber.URL,
			future,
		),
	)

	signer := &fakeSigner{err: errSign}
	notifier := NewNotifier(s, subscriber.Client(), signer)
	notifier.NotifyWrite(
		t.Context(),
		space,
		repo,
		"3lrev",
		[]byte{0x01, 0x02},
		"3lspacerev2",
		"3lspacerev1",
	)

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
		s.Register(
			t.Context(), NamespaceHabitat, space, bob,
			subscriber.URL, subscriber.URL, time.Now().Add(time.Hour),
		),
	)

	notifier := NewNotifier(s, subscriber.Client(), &fakeSigner{t: t})
	notifier.NotifyWrite(
		t.Context(),
		space,
		repo,
		"3lrev",
		[]byte{0x01, 0x02},
		"3lspacerev2",
		"3lspacerev1",
	)

	select {
	case <-delivered:
		t.Fatal("delivered notifyWrite to a non-matching registration")
	case <-time.After(200 * time.Millisecond):
	}
}

// tokenAudience reads the aud claim off a service-auth bearer token. The
// signature is not checked: these tests care about addressing, and fakeSigner
// mints a throwaway key per call.
func tokenAudience(t *testing.T, header string) string {
	t.Helper()
	raw, ok := strings.CutPrefix(header, "Bearer ")
	require.True(t, ok, "missing bearer token in %q", header)
	token, err := jwt.ParseSigned(raw)
	require.NoError(t, err)
	var claims jwt.Claims
	require.NoError(t, token.UnsafeClaimsWithoutVerification(&claims))
	require.Len(t, claims.Audience, 1)
	return claims.Audience[0]
}

// captureAudience runs one notifyWrite through a subscriber that records the
// audience of the delivery it receives, returning that audience and the
// subscriber's endpoint URL.
func captureAudience(t *testing.T, register func(Store, string) error) (string, string) {
	t.Helper()
	s := newTestStore(t)

	audiences := make(chan string, 1)
	subscriber := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		audiences <- tokenAudience(t, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(subscriber.Close)

	require.NoError(t, register(s, subscriber.URL))

	NewNotifier(s, subscriber.Client(), &fakeSigner{t: t}).
		NotifyWrite(t.Context(), space, repo, "3lrev", []byte{0x01, 0x02}, "3lspacerev2", "3lspacerev1")

	select {
	case aud := <-audiences:
		return aud, subscriber.URL
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for notifyWrite delivery")
		return "", ""
	}
}

// TestNotifierAddressesServiceIdentifier pins that a subscriber which registered
// a service identifier is addressed by it, not by the endpoint it resolved to.
func TestNotifierAddressesServiceIdentifier(t *testing.T) {
	aud, endpoint := captureAudience(t, func(s Store, ep string) error {
		return s.Register(
			t.Context(), NamespaceHabitat, space, "", syncerService, ep, time.Now().Add(time.Hour),
		)
	})
	require.Equal(t, syncerService, aud)
	require.NotEqual(t, endpoint, aud, "service identifier must not be the endpoint URL")
}

// TestNotifierAddressesLegacyEndpoint pins that a registration made through the
// deprecated endpoint field is still delivered to, addressed by its URL.
func TestNotifierAddressesLegacyEndpoint(t *testing.T) {
	aud, endpoint := captureAudience(t, func(s Store, ep string) error {
		return s.Register(
			t.Context(),
			NamespaceHabitat,
			space,
			"",
			ep,
			ep,
			time.Now().Add(time.Hour),
		)
	})
	require.Equal(t, endpoint, aud)
}

// TestNotifierDeliversAtprotoNamespace pins that a com.atproto.space
// registration is delivered com.atproto.space.notifyWrite, addressed to the
// bare DID of its service identifier.
func TestNotifierDeliversAtprotoNamespace(t *testing.T) {
	s := newTestStore(t)

	type delivery struct{ path, aud, lxm string }
	received := make(chan delivery, 1)
	subscriber := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		token, err := jwt.ParseSigned(raw)
		require.NoError(t, err)
		var claims struct {
			jwt.Claims
			Lxm string `json:"lxm"`
		}
		require.NoError(t, token.UnsafeClaimsWithoutVerification(&claims))
		received <- delivery{r.URL.Path, claims.Audience[0], claims.Lxm}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(subscriber.Close)

	require.NoError(t, s.Register(
		t.Context(), NamespaceAtproto, space, "", syncerService, subscriber.URL,
		time.Now().Add(time.Hour),
	))
	NewNotifier(s, subscriber.Client(), &fakeSigner{t: t}).
		NotifyWrite(t.Context(), space, repo, "3lrev", []byte{0x01}, "3lspacerev2", "3lspacerev1")

	select {
	case d := <-received:
		require.Equal(t, "/xrpc/com.atproto.space.notifyWrite", d.path)
		require.Equal(t, "com.atproto.space.notifyWrite", d.lxm)
		require.Equal(t, "did:web:sync.example.com", d.aud)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for notifyWrite delivery")
	}
}

// fakeDeletedSpaces is a [DeletedSpaces] reporting deleted for every space in
// it.
type fakeDeletedSpaces map[habitat_syntax.SpaceURI]bool

func (f fakeDeletedSpaces) IsSpaceDeleted(
	_ context.Context,
	uri habitat_syntax.SpaceURI,
) (bool, error) {
	return f[uri], nil
}

// setupDeletion registers a syncer whose endpoint answers each delivery with
// the next of statuses (200 once they run out), and queues a pending
// notifySpaceDeleted to it the way NotifySpaceDeleted does. It returns the
// deliverer, on a clock the test moves, and the pending delivery.
func setupDeletion(
	t *testing.T,
	expiresIn time.Duration,
	statuses ...int,
) (*Deliverer, PendingDeletion, *int) {
	t.Helper()
	s := newTestStore(t)
	delivered := 0
	subscriber := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		status := http.StatusOK
		if delivered < len(statuses) {
			status = statuses[delivered]
		}
		delivered++
		w.WriteHeader(status)
	}))
	t.Cleanup(subscriber.Close)

	now := time.Now()
	require.NoError(t, s.Register(
		t.Context(), NamespaceHabitat, space, "", subscriber.URL, subscriber.URL,
		now.Add(expiresIn),
	))
	d := NewNotifier(s, subscriber.Client(), &fakeSigner{t: t})
	d.now = func() time.Time { return now }

	regs, err := s.ListForSpace(t.Context(), space)
	require.NoError(t, err)
	pending, err := s.QueueSpaceDeleted(t.Context(), space, regs, d.leaseUntil())
	require.NoError(t, err)
	require.Len(t, pending, 1)
	return d, pending[0], &delivered
}

// advance moves d's clock forward by by.
func advance(d *Deliverer, by time.Duration) {
	at := d.now().Add(by)
	d.now = func() time.Time { return at }
}

func pendingDeletions(t *testing.T, d *Deliverer) []PendingDeletion {
	t.Helper()
	due, err := d.store.DueSpaceDeleted(t.Context(), math.MaxInt64, 100)
	require.NoError(t, err)
	return due
}

func TestNotifierRetriesSpaceDeletedUntilDelivered(t *testing.T) {
	d, p, delivered := setupDeletion(
		t, 24*time.Hour, http.StatusServiceUnavailable, http.StatusTooManyRequests,
	)
	deleted := fakeDeletedSpaces{space: true}

	// The first attempt fails transiently, so the delivery stays queued with
	// a backoff of 30s to 1m.
	d.attemptSpaceDeleted(t.Context(), p, nil)
	require.Equal(t, 1, *delivered)
	pending := pendingDeletions(t, d)
	require.Len(t, pending, 1)
	require.Equal(t, 1, pending[0].Attempts)
	require.Greater(t, pending[0].RetryAt, d.now().Add(29*time.Second).UnixMilli())

	// Nothing is due before the backoff elapses.
	d.retryDue(t.Context(), deleted)
	require.Equal(t, 1, *delivered)

	// The second fails transiently too and backs off further (1-2m).
	advance(d, time.Minute)
	d.retryDue(t.Context(), deleted)
	require.Equal(t, 2, *delivered)
	pending = pendingDeletions(t, d)
	require.Len(t, pending, 1)
	require.Equal(t, 2, pending[0].Attempts)
	require.Greater(t, pending[0].RetryAt, d.now().Add(59*time.Second).UnixMilli())

	// The third succeeds and the delivery is done.
	advance(d, 2*time.Minute)
	d.retryDue(t.Context(), deleted)
	require.Equal(t, 3, *delivered)
	require.Empty(t, pendingDeletions(t, d))
}

func TestNotifierStopsSpaceDeletedOnPermanentError(t *testing.T) {
	d, p, delivered := setupDeletion(t, 24*time.Hour, http.StatusNotFound)

	d.attemptSpaceDeleted(t.Context(), p, nil)
	require.Equal(t, 1, *delivered)
	require.Empty(t, pendingDeletions(t, d))
}

func TestNotifierStopsSpaceDeletedWhenRegistrationExpires(t *testing.T) {
	d, p, delivered := setupDeletion(t, 90*time.Second, http.StatusBadGateway)
	deleted := fakeDeletedSpaces{space: true}

	d.attemptSpaceDeleted(t.Context(), p, nil)
	require.Equal(t, 1, *delivered)
	pending := pendingDeletions(t, d)
	require.Len(t, pending, 1)
	// The backoff never runs past the registration's expiry.
	require.LessOrEqual(t, pending[0].RetryAt, pending[0].ExpiresAt.UnixMilli())

	// Once the registration lapses the delivery is dropped unsent.
	advance(d, 2*time.Minute)
	d.retryDue(t.Context(), deleted)
	require.Equal(t, 1, *delivered)
	require.Empty(t, pendingDeletions(t, d))
}

func TestNotifierStopsSpaceDeletedWhenSpaceRecreated(t *testing.T) {
	d, p, delivered := setupDeletion(t, 24*time.Hour, http.StatusServiceUnavailable)

	d.attemptSpaceDeleted(t.Context(), p, nil)
	require.Equal(t, 1, *delivered)

	// The space is no longer deleted: delivering now would purge the
	// syncer's copy of the new one.
	advance(d, time.Hour)
	d.retryDue(t.Context(), fakeDeletedSpaces{})
	require.Equal(t, 1, *delivered)
	require.Empty(t, pendingDeletions(t, d))
}

func TestNotifierRunStopsWithContext(t *testing.T) {
	d, _, _ := setupDeletion(t, 24*time.Hour)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, d.Run(ctx, fakeDeletedSpaces{}), context.Canceled)
}
