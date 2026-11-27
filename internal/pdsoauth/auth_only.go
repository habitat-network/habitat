package pdsoauth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/keyval"
)

const authOnlyScope = "atproto"

// AuthOnly verifies the identity of a personal PDS account without creating a
// personal write session. The caller binds the returned state and the expected
// actor to its own authorization request; an identity is not a group grant.
type AuthOnly struct {
	app *App
}

func NewAuthOnly(cfg Config, kv keyval.Store, dir identity.Directory) (*AuthOnly, error) {
	store := authOnlyStore{requests: kv.Namespace("pdsoauth-auth-only-requests")}
	app, err := newApp(cfg, store, dir, []string{authOnlyScope})
	if err != nil {
		return nil, err
	}
	return &AuthOnly{app: app}, nil
}

// Start begins authentication for an already selected personal DID. The
// caller must retain state with the outer authorization request.
func (a *AuthOnly) Start(
	ctx context.Context,
	actor syntax.DID,
) (redirect, state string, err error) {
	if _, err := syntax.ParseDID(actor.String()); err != nil {
		return "", "", fmt.Errorf("invalid personal DID: %w", err)
	}
	ctx, savedState := WithAuthState(ctx)
	redirect, err = a.app.StartAuthFlow(ctx, actor.String())
	if err != nil {
		return "", "", err
	}
	// Indigo does not propagate a pending-request store write failure from
	// StartAuthFlow. The store only reports state after successfully saving it.
	if savedState() == "" {
		return "", "", errors.New("auth-only PDS request was not saved")
	}
	return redirect, savedState(), nil
}

// Verify redeems the callback once and returns only the verified personal DID.
// expectedState and expectedActor must come from the outer request, not the
// incoming callback. If redemption fails, start a fresh authentication.
func (a *AuthOnly) Verify(
	ctx context.Context,
	params url.Values,
	expectedState string,
	expectedActor syntax.DID,
) (syntax.DID, error) {
	if states := params["state"]; expectedState == "" || len(states) != 1 ||
		states[0] != expectedState {
		return "", errors.New("PDS callback does not match the pending authentication")
	}
	if _, err := syntax.ParseDID(expectedActor.String()); err != nil {
		return "", fmt.Errorf("invalid expected personal DID: %w", err)
	}
	session, err := a.app.ProcessCallback(ctx, params)
	if err != nil {
		return "", fmt.Errorf("verify auth-only PDS callback: %w", err)
	}
	if session.AccountDID != expectedActor {
		return "", errors.New("PDS authenticated a different personal account")
	}
	if !slices.Equal(session.Scopes, []string{authOnlyScope}) {
		return "", errors.New("PDS did not grant exactly the authentication-only scope")
	}
	return session.AccountDID, nil
}

// authOnlyStore atomically consumes pending requests and never saves tokens.
// Indigo calls SaveSession before it returns the verified callback result.
type authOnlyStore struct {
	requests keyval.Store
}

func (s authOnlyStore) GetAuthRequestInfo(
	ctx context.Context,
	state string,
) (*oauth.AuthRequestData, error) {
	var info oauth.AuthRequestData
	if err := s.requests.Consume(ctx, state, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (s authOnlyStore) SaveAuthRequestInfo(ctx context.Context, info oauth.AuthRequestData) error {
	if err := s.requests.PutIfAbsent(ctx, info.State, info, authRequestTTL); err != nil {
		return err
	}
	reportAuthState(ctx, info.State)
	return nil
}

func (authOnlyStore) DeleteAuthRequestInfo(context.Context, string) error { return nil }
func (authOnlyStore) SaveSession(context.Context, oauth.ClientSessionData) error {
	return nil
}

func (authOnlyStore) GetSession(
	context.Context,
	syntax.DID,
	string,
) (*oauth.ClientSessionData, error) {
	return nil, ErrSessionNotFound
}
func (authOnlyStore) DeleteSession(context.Context, syntax.DID, string) error { return nil }
