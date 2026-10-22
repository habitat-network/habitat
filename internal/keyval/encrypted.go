package keyval

import (
	"context"
	"fmt"
	"time"

	"github.com/habitat-network/habitat/internal/encrypt"
)

// EncryptedKV mirrors [KV], except values are encrypted with
// [encrypt.EncryptCBOR] before they reach the backing store.
type EncryptedKV interface {
	// PutEncrypted encrypts value and stores it under key, expiring it ttl from now, or never if ttl is [NoExpiry].
	PutEncrypted(ctx context.Context, key string, value any, ttl time.Duration) error
	// GetEncrypted decrypts the value stored under key into dst. It returns
	// [ErrNotFound] if the key was never written or has expired.
	GetEncrypted(ctx context.Context, key string, dst any) error
}

// EncryptedStore mirrors [Store] with encrypted values. Delete needs no
// encryption, so callers use the backing Store's for that.
type EncryptedStore interface {
	EncryptedKV
	// PutIfAbsentEncrypted is [Store.PutIfAbsent] over an encrypted value.
	PutIfAbsentEncrypted(ctx context.Context, key string, value any, ttl time.Duration) error
	// ConsumeEncrypted is [Store.Consume] over an encrypted value.
	ConsumeEncrypted(ctx context.Context, key string, dst any) error
	// Namespace returns a store bound to ns that encrypts with the same key, as
	// [Store.Namespace] does for plain stores.
	Namespace(ns string) EncryptedStore
}

type encryptedStore struct {
	store Store
	key   []byte
}

var _ EncryptedStore = (*encryptedStore)(nil)

// NewEncrypted returns an [EncryptedStore] persisting through store, encrypting
// every value with key, which must be 32 bytes. Like [New], it returns a root
// store: consumers are handed a child from Namespace, never the root.
func NewEncrypted(store Store, key []byte) (EncryptedStore, error) {
	// Probe the key now so a bad one fails at construction, not on first use.
	if _, err := encrypt.EncryptCBOR(nil, key); err != nil {
		return nil, fmt.Errorf("keyval: invalid encryption key: %w", err)
	}
	return &encryptedStore{store: store, key: key}, nil
}

func (s *encryptedStore) Namespace(ns string) EncryptedStore {
	return &encryptedStore{store: s.store.Namespace(ns), key: s.key}
}

func (s *encryptedStore) PutEncrypted(
	ctx context.Context,
	key string,
	value any,
	ttl time.Duration,
) error {
	token, err := encrypt.EncryptCBOR(value, s.key)
	if err != nil {
		return fmt.Errorf("encrypt keyval value: %w", err)
	}
	return s.store.Put(ctx, key, token, ttl)
}

func (s *encryptedStore) PutIfAbsentEncrypted(
	ctx context.Context,
	key string,
	value any,
	ttl time.Duration,
) error {
	token, err := encrypt.EncryptCBOR(value, s.key)
	if err != nil {
		return fmt.Errorf("encrypt keyval value: %w", err)
	}
	return s.store.PutIfAbsent(ctx, key, token, ttl)
}

func (s *encryptedStore) GetEncrypted(ctx context.Context, key string, dst any) error {
	var token string
	if err := s.store.Get(ctx, key, &token); err != nil {
		return err
	}
	return s.decrypt(token, dst)
}

func (s *encryptedStore) ConsumeEncrypted(ctx context.Context, key string, dst any) error {
	var token string
	if err := s.store.Consume(ctx, key, &token); err != nil {
		return err
	}
	return s.decrypt(token, dst)
}

func (s *encryptedStore) decrypt(token string, dst any) error {
	if dst == nil {
		return errNilDst
	}
	if err := encrypt.DecryptCBOR(token, s.key, dst); err != nil {
		return fmt.Errorf("decrypt keyval value: %w", err)
	}
	return nil
}
