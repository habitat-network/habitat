package atprotoauth

import (
	"context"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"gorm.io/gorm"
)

// asSigningKey is the persisted signing key for this authorization server.
// One row only; the fixed primary key keeps the upsert race-free if two
// instances start at once. The key signs every token this AS issues, and its
// public half is published at /atproto-oauth/jwks.
type asSigningKey struct {
	ID                  string `gorm:"primaryKey"`
	PublicKeyMultibase  string
	PrivateKeyMultibase string
}

const asSigningKeyRowID = "default"

// EnsureASSigningKey returns the authorization server's signing key, generating
// and persisting one on first call. K256 (secp256k1) matches atproto's default
// signing convention — clients that already verify PLC/PDS keys can verify ours
// without curve negotiation.
//
// TODO: encrypt PrivateKeyMultibase at rest (same outstanding work as hive's
// per-identity keys; see internal/hive/store.go:76).
func EnsureASSigningKey(ctx context.Context, db *gorm.DB) (atcrypto.PrivateKeyExportable, error) {
	if err := db.WithContext(ctx).AutoMigrate(&asSigningKey{}); err != nil {
		return nil, fmt.Errorf("migrating as_signing_keys: %w", err)
	}

	var row asSigningKey
	err := db.WithContext(ctx).Where("id = ?", asSigningKeyRowID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		priv, err := atcrypto.GeneratePrivateKeyK256()
		if err != nil {
			return nil, fmt.Errorf("generating AS signing key: %w", err)
		}
		pub, err := priv.PublicKey()
		if err != nil {
			return nil, fmt.Errorf("deriving AS public key: %w", err)
		}
		row = asSigningKey{
			ID:                  asSigningKeyRowID,
			PublicKeyMultibase:  pub.Multibase(),
			PrivateKeyMultibase: priv.Multibase(),
		}
		// ON CONFLICT DO NOTHING handles the race where two processes both miss
		// on the read and both try to create. The loser's row simply isn't
		// inserted; we re-read below.
		if err := db.WithContext(ctx).
			Where("id = ?", asSigningKeyRowID).
			FirstOrCreate(&row).Error; err != nil {
			return nil, fmt.Errorf("persisting AS signing key: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("reading AS signing key: %w", err)
	}

	parsed, err := atcrypto.ParsePrivateMultibase(row.PrivateKeyMultibase)
	if err != nil {
		return nil, fmt.Errorf("parsing persisted AS signing key: %w", err)
	}
	return parsed, nil
}
