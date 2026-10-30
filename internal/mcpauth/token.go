package mcpauth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/keyval"
)

const (
	// lockWait is how long AccessToken waits on another instance's refresh
	// of the same credential before giving up.
	lockWait = 5 * time.Second
	// lockPollInterval is how often a waiting AccessToken checks whether the
	// other refresh finished.
	lockPollInterval = 100 * time.Millisecond
)

func (s *service) AccessToken(
	ctx context.Context,
	server Server,
	member syntax.DID,
) (string, error) {
	key := connectionKey(server, member)
	if tok, ok, err := s.cachedAccessToken(ctx, key, server.URL); err != nil {
		return "", err
	} else if ok {
		return tok, nil
	}
	// Callers in this process share one refresh; refreshLocking keeps other
	// pear instances out too.
	tok, err, _ := s.refreshes.Do(key, func() (any, error) {
		return s.refreshLocking(ctx, server, member)
	})
	if err != nil {
		return "", err
	}
	return tok.(string), nil
}

func (s *service) Invalidate(ctx context.Context, server Server, member syntax.DID) error {
	if err := s.deleteIfPresent(ctx, tokenPrefix+connectionKey(server, member)); err != nil {
		return fmt.Errorf("delete mcp access token: %w", err)
	}
	return nil
}

// refreshLocking refreshes member's access token for server while holding
// the credential's keyval lock. Refresh tokens may rotate on every use, so
// two concurrent refreshes would leave whichever finished first holding a
// revoked token. If another instance holds the lock, it waits for that
// instance's token to appear instead.
//
// keyval has no compare-and-swap, so the lock is a row created with
// PutIfAbsent that expires after lockTTL, in case its holder dies.
func (s *service) refreshLocking(
	ctx context.Context,
	server Server,
	member syntax.DID,
) (string, error) {
	key := connectionKey(server, member)
	deadline := time.Now().Add(lockWait)
	for {
		if tok, ok, err := s.cachedAccessToken(ctx, key, server.URL); err != nil {
			return "", err
		} else if ok {
			return tok, nil
		}
		err := s.plain.PutIfAbsent(ctx, lockPrefix+key, struct{}{}, lockTTL)
		if err == nil {
			return s.refreshHoldingLock(ctx, server, member)
		}
		if !errors.Is(err, keyval.ErrAlreadyExists) {
			return "", fmt.Errorf("take mcp refresh lock: %w", err)
		}
		if time.Now().After(deadline) {
			return "", ErrRefreshInProgress
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(lockPollInterval):
		}
	}
}

// refreshHoldingLock refreshes once the caller has taken the credential's
// lock, and releases it.
func (s *service) refreshHoldingLock(
	ctx context.Context,
	server Server,
	member syntax.DID,
) (string, error) {
	key := connectionKey(server, member)
	defer func() {
		// Released even if ctx is done, or waiters would sit out lockTTL.
		_ = s.deleteIfPresent(context.WithoutCancel(ctx), lockPrefix+key)
	}()
	// The previous holder may have stored a fresh token between the caller's
	// check and taking the lock.
	if tok, ok, err := s.cachedAccessToken(ctx, key, server.URL); err != nil {
		return "", err
	} else if ok {
		return tok, nil
	}
	return s.refresh(ctx, server, member)
}

// refresh redeems member's refresh token for server. The caller holds the
// lock.
func (s *service) refresh(ctx context.Context, server Server, member syntax.DID) (string, error) {
	key := connectionKey(server, member)
	cred, err := s.loadCredential(ctx, server, member)
	if err != nil {
		return "", err
	}
	if cred.NeedsReauth || cred.RefreshToken == "" {
		return "", ErrNeedsReauth
	}

	resp, err := requestToken(ctx, s.httpClient, cred.Endpoints.TokenURL, cred.Client, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {cred.RefreshToken},
		"resource":      {cred.Resource},
	})
	var te *tokenError
	if errors.As(err, &te) && te.Code == "invalid_grant" {
		cred.NeedsReauth = true
		cred.RefreshToken = ""
		if err := s.secrets.PutEncrypted(
			ctx,
			credentialPrefix+key,
			cred,
			keyval.NoExpiry,
		); err != nil {
			return "", fmt.Errorf("store mcp credential: %w", err)
		}
		return "", fmt.Errorf("%w: %v", ErrNeedsReauth, te)
	} else if err != nil {
		return "", err
	}

	// The server may rotate the refresh token, or keep the old one valid by
	// not returning a new one.
	if resp.RefreshToken != "" {
		cred.RefreshToken = resp.RefreshToken
	}
	return s.save(ctx, key, cred, resp)
}
