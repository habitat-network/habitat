package org

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/stretchr/testify/require"
)

// fakeMgr lets tests control isMember without a real DB.
type fakeMgr struct {
	members map[string]bool
	err     error
}

func (f *fakeMgr) isMember(_ context.Context, did syntax.DID) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.members[did.String()], nil
}

func (f *fakeMgr) BootstrapAdmin(_ context.Context, _ string, _ syntax.DID) error   { panic("unused") }
func (f *fakeMgr) GetAdmins(_ context.Context) ([]syntax.DID, error)                { panic("unused") }
func (f *fakeMgr) GetMembers(_ context.Context) ([]syntax.DID, error)               { panic("unused") }
func (f *fakeMgr) AddAdmin(_ context.Context, _ syntax.DID, _ syntax.DID) error     { panic("unused") }
func (f *fakeMgr) AddMembers(_ context.Context, _ syntax.DID, _ []syntax.DID) error { panic("unused") }
func (f *fakeMgr) RemoveAdmin(_ context.Context, _ syntax.DID, _ syntax.DID) error  { panic("unused") }
func (f *fakeMgr) RemoveMembers(_ context.Context, _ syntax.DID, _ []syntax.DID) error {
	panic("unused")
}

var _ Manager = &fakeMgr{}

// fakeAuthMethod always authenticates as a fixed DID.
type fakeAuthMethod struct{ did syntax.DID }

func (f *fakeAuthMethod) CanHandle(_ *http.Request) bool { return true }
func (f *fakeAuthMethod) Validate(_ http.ResponseWriter, _ *http.Request, _ ...string) (syntax.DID, bool) {
	return f.did, true
}
func (f *fakeAuthMethod) ValidateRaw(_ context.Context, _ string, _ ...string) (syntax.DID, bool, error) {
	return f.did, true, nil
}

func TestMiddleware_NoCaller(t *testing.T) {
	reached := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	})

	// No authn wrapper — FromContext returns false, middleware returns early.
	handler := Middleware(&fakeMgr{})(next)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))

	require.False(t, reached)
}

func TestMiddleware_MemberAllowed(t *testing.T) {
	did := syntax.DID("did:plc:member1")
	mgr := &fakeMgr{members: map[string]bool{did.String(): true}}

	reached := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})

	handler := authn.Middleware(&fakeAuthMethod{did})(Middleware(mgr)(next))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))

	require.True(t, reached)
	require.Equal(t, http.StatusOK, w.Result().StatusCode)
}

func TestMiddleware_NonMemberRejected(t *testing.T) {
	did := syntax.DID("did:plc:outsider")
	mgr := &fakeMgr{members: map[string]bool{}}

	reached := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	})

	handler := authn.Middleware(&fakeAuthMethod{did})(Middleware(mgr)(next))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))

	require.False(t, reached)
	require.Equal(t, http.StatusMethodNotAllowed, w.Result().StatusCode)
}

func TestMiddleware_IsMemberError(t *testing.T) {
	did := syntax.DID("did:plc:anyone")
	mgr := &fakeMgr{err: errors.New("db down")}

	reached := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	})

	handler := authn.Middleware(&fakeAuthMethod{did})(Middleware(mgr)(next))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))

	require.False(t, reached)
	require.Equal(t, http.StatusInternalServerError, w.Result().StatusCode)
}
