package pdscred

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/keyval"
)

type PDSCredentialStore interface {
	UpsertCredentials(ctx context.Context, did syntax.DID, credentials *Credentials) error
	GetCredentials(ctx context.Context, did syntax.DID) (*Credentials, error)
}

func NewPDSCredentialStore(kv keyval.EncryptedStore) PDSCredentialStore {
	return &pdsCredentialStore{kv: kv}
}

type pdsCredentialStore struct {
	kv keyval.EncryptedStore
}

// storedCredentials is the encrypted value kept per DID. These credentials are
// reused across multiple OAuth sessions and replaced on every sign-in.
type storedCredentials struct {
	// Tokens received from the AT Protocol PDS (via OAuth client)
	AccessToken  string
	RefreshToken string
	// DpopKey is the serialized ECDSA private key for proof-of-possession.
	DpopKey []byte
}

type Credentials struct {
	AccessToken  string
	RefreshToken string
	DpopKey      *ecdsa.PrivateKey
}

func (p *pdsCredentialStore) GetCredentials(
	ctx context.Context,
	did syntax.DID,
) (*Credentials, error) {
	var stored storedCredentials
	if err := p.kv.GetEncrypted(ctx, did.String(), &stored); err != nil {
		return nil, fmt.Errorf("user credentials not found: %w", err)
	}
	dpopKey, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), stored.DpopKey)
	if err != nil {
		return nil, fmt.Errorf("failed to parse dpop key: %w", err)
	}
	return &Credentials{
		AccessToken:  stored.AccessToken,
		RefreshToken: stored.RefreshToken,
		DpopKey:      dpopKey,
	}, nil
}

// UpsertCredentials implements [PDSCredentialStore].
func (p *pdsCredentialStore) UpsertCredentials(
	ctx context.Context,
	did syntax.DID,
	tokenInfo *Credentials,
) error {
	dpopKey, err := tokenInfo.DpopKey.Bytes()
	if err != nil {
		return fmt.Errorf("failed to get dpop key bytes: %w", err)
	}
	if err := p.kv.PutEncrypted(ctx, did.String(), storedCredentials{
		AccessToken:  tokenInfo.AccessToken,
		RefreshToken: tokenInfo.RefreshToken,
		DpopKey:      dpopKey,
	}, keyval.NoExpiry); err != nil {
		return fmt.Errorf("failed to save user credentials: %w", err)
	}
	return nil
}
