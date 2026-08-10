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
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
)

var errSign = errors.New("sign failed")

// fakeDirectory resolves a fixed set of identities, standing in for a real
// identity.Directory in tests that don't need actual DID resolution.
type fakeDirectory struct {
	idents map[syntax.DID]*identity.Identity
}

func (d *fakeDirectory) LookupDID(_ context.Context, did syntax.DID) (*identity.Identity, error) {
	ident, ok := d.idents[did]
	if !ok {
		return nil, identity.ErrDIDNotFound
	}
	return ident, nil
}

func (d *fakeDirectory) LookupHandle(context.Context, syntax.Handle) (*identity.Identity, error) {
	return nil, identity.ErrHandleNotFound
}

func (d *fakeDirectory) Lookup(
	ctx context.Context,
	atid syntax.AtIdentifier,
) (*identity.Identity, error) {
	if did, err := atid.AsDID(); err == nil {
		return d.LookupDID(ctx, did)
	}
	return nil, identity.ErrDIDNotFound
}

func (d *fakeDirectory) Purge(context.Context, syntax.AtIdentifier) error { return nil }

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
	notifier := NewNotifier(s, subscriber.Client(), signer, &fakeDirectory{})
	notifier.NotifyWrite(t.Context(), space, repo, "3lrev", []byte{0x01, 0x02})

	for range 2 {
		select {
		case d := <-received:
			require.Equal(t, space.String(), d.in.Space)
			require.Equal(t, repo.String(), d.in.Repo)
			require.Equal(t, "3lrev", d.in.Rev)
			require.Equal(t, "AQI=", d.in.Hash)
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
	notifier := NewNotifier(s, subscriber.Client(), signer, &fakeDirectory{})
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
	notifier := NewNotifier(s, http.DefaultClient, signer, &fakeDirectory{})

	// With no registrations, neither path should sign or deliver anything.
	notifier.NotifyWrite(t.Context(), space, repo, "3lrev", []byte{0x01, 0x02})
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
	notifier := NewNotifier(s, subscriber.Client(), signer, &fakeDirectory{})
	notifier.NotifyWrite(t.Context(), space, repo, "3lrev", []byte{0x01, 0x02})

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

	notifier := NewNotifier(s, subscriber.Client(), &fakeSigner{t: t}, &fakeDirectory{})
	notifier.NotifyWrite(t.Context(), space, repo, "3lrev", []byte{0x01, 0x02})

	select {
	case <-delivered:
		t.Fatal("delivered notifyWrite to a non-matching registration")
	case <-time.After(200 * time.Millisecond):
	}
}

// TestRegisterAuthorityRegistersHostEndpoint pins that a shared space's
// authority is auto-subscribed to a repo through its published
// "#atproto_space_host" service, per the proposal's auto-registration
// behavior for a repo's first write.
func TestRegisterAuthorityRegistersHostEndpoint(t *testing.T) {
	s := newTestStore(t)

	dir := &fakeDirectory{idents: map[syntax.DID]*identity.Identity{
		space.SpaceOwner(): {
			DID: space.SpaceOwner(),
			Services: map[string]identity.ServiceEndpoint{
				"atproto_space_host": {URL: "https://authority.example.com"},
			},
		},
	}}
	notifier := NewNotifier(s, http.DefaultClient, &fakeSigner{t: t}, dir)
	notifier.RegisterAuthority(t.Context(), space, repo)

	regs, err := s.ListForRepo(t.Context(), space, repo)
	require.NoError(t, err)
	require.Len(t, regs, 1)
	require.Equal(t, "https://authority.example.com", regs[0].Endpoint)
}

// TestRegisterAuthoritySkipsOwnSpace pins that a repo owned by the space's own
// authority is never auto-registered: there is no separate authority to
// notify.
func TestRegisterAuthoritySkipsOwnSpace(t *testing.T) {
	s := newTestStore(t)

	notifier := NewNotifier(s, http.DefaultClient, &fakeSigner{t: t}, &fakeDirectory{})
	notifier.RegisterAuthority(t.Context(), space, space.SpaceOwner())

	regs, err := s.ListForRepo(t.Context(), space, space.SpaceOwner())
	require.NoError(t, err)
	require.Empty(t, regs)
}

// TestRegisterAuthorityNoHostService pins that an authority publishing no
// "#atproto_space_host" service is left unregistered rather than erroring.
func TestRegisterAuthorityNoHostService(t *testing.T) {
	s := newTestStore(t)

	dir := &fakeDirectory{idents: map[syntax.DID]*identity.Identity{
		space.SpaceOwner(): {DID: space.SpaceOwner()},
	}}
	notifier := NewNotifier(s, http.DefaultClient, &fakeSigner{t: t}, dir)
	notifier.RegisterAuthority(t.Context(), space, repo)

	regs, err := s.ListForRepo(t.Context(), space, repo)
	require.NoError(t, err)
	require.Empty(t, regs)
}
