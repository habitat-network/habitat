package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type webhookDelivery struct {
	Space  string
	Token  string
	Update map[string]any
}

// webhookReceiver collects the notifySpaceUpdate bodies posted to it.
func webhookReceiver(t *testing.T) (*httptest.Server, <-chan webhookDelivery) {
	t.Helper()
	got := make(chan webhookDelivery, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/xrpc/com.atproto.space.notifySpaceUpdate", r.URL.Path)
		require.Empty(t, r.Header.Get("Authorization"))
		var d webhookDelivery
		require.NoError(t, json.NewDecoder(r.Body).Decode(&d))
		got <- d
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func TestNotifierDeliversWriteAndRemovalToWebhooks(t *testing.T) {
	s := newTestStore(t)
	srv, got := webhookReceiver(t)
	require.NoError(t, s.RegisterWebhook(
		t.Context(), space, srv.URL, "tok", time.Now().Add(time.Hour),
	))
	d := NewNotifier(s, http.DefaultClient, &fakeSigner{t: t}).WithWebhookClient(srv.Client())

	d.NotifyWrite(t.Context(), space, repo, "3lrev", []byte{1}, "3lspacerev2", "3lspacerev1")
	w := <-got
	require.Equal(t, space.String(), w.Space)
	require.Equal(t, "tok", w.Token)
	require.Equal(t, "com.atproto.space.defs#repoWrite", w.Update["$type"])
	require.Equal(t, repo.String(), w.Update["repo"])
	require.Equal(t, "3lspacerev2", w.Update["spaceRev"])

	d.NotifyWriterRemoved(t.Context(), space, repo, "3lspacerev3", "3lspacerev2")
	r := <-got
	require.Equal(t, "com.atproto.space.defs#repoRemoved", r.Update["$type"])
	require.Equal(t, "3lspacerev2", r.Update["prevSpaceRev"])
}

func TestNotifierSendsRegistrationExpiredOnceAfterExpiry(t *testing.T) {
	s := newTestStore(t)
	srv, got := webhookReceiver(t)
	now := time.Now()
	require.NoError(t, s.RegisterWebhook(t.Context(), space, srv.URL, "tok", now.Add(-time.Minute)))
	d := NewNotifier(s, http.DefaultClient, &fakeSigner{t: t}).WithWebhookClient(srv.Client())
	d.now = func() time.Time { return now }

	d.NotifyWrite(t.Context(), space, repo, "3lrev", nil, "3lspacerev2", "3lspacerev1")
	require.Equal(t,
		"com.atproto.space.notifySpaceUpdate#registrationExpired", (<-got).Update["$type"])

	// The notice is recorded after delivery; wait for it, then expect silence.
	require.Eventually(t, func() bool {
		hooks, err := s.ListWebhooks(t.Context(), space)
		return err == nil && len(hooks) == 1 && hooks[0].ExpiryNotified
	}, 5*time.Second, 10*time.Millisecond)
	d.NotifyWrite(t.Context(), space, repo, "3lrev2", nil, "3lspacerev3", "3lspacerev2")
	require.Empty(t, got)
}

func TestNotifierDeletionReachesLapsedWebhooks(t *testing.T) {
	s := newTestStore(t)
	srv, got := webhookReceiver(t)
	require.NoError(t, s.RegisterWebhook(
		t.Context(), space, srv.URL, "tok", time.Now().Add(-time.Hour),
	))
	d := NewNotifier(s, http.DefaultClient, &fakeSigner{t: t}).WithWebhookClient(srv.Client())

	d.NotifySpaceDeleted(t.Context(), space)
	require.Equal(t,
		"com.atproto.space.notifySpaceUpdate#spaceDeleted", (<-got).Update["$type"])
}

func TestConfirmRegistrationRequiresEchoedChallenge(t *testing.T) {
	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Challenge string }
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		require.NoError(t, json.NewEncoder(w).Encode(map[string]string{"challenge": in.Challenge}))
	}))
	t.Cleanup(echo.Close)
	require.NoError(t, ConfirmRegistration(t.Context(), echo.Client(), space, echo.URL, "tok"))

	wrong := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		require.NoError(t, json.NewEncoder(w).Encode(map[string]string{"challenge": "nope"}))
	}))
	t.Cleanup(wrong.Close)
	require.ErrorIs(t,
		ConfirmRegistration(t.Context(), wrong.Client(), space, wrong.URL, "tok"),
		ErrRegistrationNotConfirmed)
}
