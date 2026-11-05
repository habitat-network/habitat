// Package pdsoauth is Habitat's atproto OAuth client of its members' PDSes,
// built on indigo's oauth.ClientApp. It supplies the pieces indigo leaves to
// the application: a keyval-backed session/auth-request store and a factory of
// authenticated HTTP clients for a DID's PDS.
package pdsoauth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/keyval"
)

// authRequestTTL bounds how long a member has to finish an authorization at
// their PDS before the pending request (PKCE verifier, DPoP key) is dropped.
const authRequestTTL = 10 * time.Minute

// ErrSessionNotFound is returned when a DID has no stored PDS session.
var ErrSessionNotFound = errors.New("pdsoauth: session not found")

// Store implements [oauth.ClientAuthStore]. Sessions hold the member's tokens
// and DPoP key, so they live in the encrypted keyval without expiry. Pending
// authorization requests are ephemeral, so they live in the regular keyval and
// expire on their own.
//
// Like the credentials it replaces, a DID has at most one session: signing in
// again replaces it, and the sessionID arguments are ignored. This lets the
// forwarding paths find a DID's session without knowing its ID.
type Store struct {
	sessions    keyval.EncryptedStore
	sessionsDel keyval.Store
	requests    keyval.Store
}

var _ oauth.ClientAuthStore = (*Store)(nil)

// NewStore returns a Store keeping sessions in sessions and pending
// authorization requests in requests. Deleting needs no decryption, so
// sessionsPlain must be the same namespace as sessions, unencrypted.
func NewStore(
	sessions keyval.EncryptedStore,
	sessionsPlain keyval.Store,
	requests keyval.Store,
) *Store {
	return &Store{sessions: sessions, sessionsDel: sessionsPlain, requests: requests}
}

func (s *Store) GetSession(
	ctx context.Context,
	did syntax.DID,
	_ string,
) (*oauth.ClientSessionData, error) {
	var data oauth.ClientSessionData
	if err := s.sessions.GetEncrypted(ctx, did.String(), &data); err != nil {
		if errors.Is(err, keyval.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, did)
		}
		return nil, fmt.Errorf("get session: %w", err)
	}
	return &data, nil
}

func (s *Store) SaveSession(ctx context.Context, sess oauth.ClientSessionData) error {
	return s.sessions.PutEncrypted(ctx, sess.AccountDID.String(), sess, keyval.NoExpiry)
}

func (s *Store) DeleteSession(ctx context.Context, did syntax.DID, _ string) error {
	if err := s.sessionsDel.Delete(ctx, did.String()); err != nil &&
		!errors.Is(err, keyval.ErrNotFound) {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (s *Store) GetAuthRequestInfo(
	ctx context.Context,
	state string,
) (*oauth.AuthRequestData, error) {
	var info oauth.AuthRequestData
	if err := s.requests.Get(ctx, state, &info); err != nil {
		return nil, fmt.Errorf("get auth request: %w", err)
	}
	return &info, nil
}

func (s *Store) SaveAuthRequestInfo(ctx context.Context, info oauth.AuthRequestData) error {
	reportAuthState(ctx, info.State)
	// Create-only, as the interface requires.
	if err := s.requests.PutIfAbsent(ctx, info.State, info, authRequestTTL); err != nil {
		return fmt.Errorf("save auth request: %w", err)
	}
	return nil
}

func (s *Store) DeleteAuthRequestInfo(ctx context.Context, state string) error {
	if err := s.requests.Delete(ctx, state); err != nil && !errors.Is(err, keyval.ErrNotFound) {
		return fmt.Errorf("delete auth request: %w", err)
	}
	return nil
}
