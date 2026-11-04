package hive

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Generate the opaqueID used in the did:web: identities that are minted
const opaqueIDAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

func generateOpaqueID() (string, error) {
	b := make([]byte, 6)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(opaqueIDAlphabet))))
		if err != nil {
			return "", err
		}
		b[i] = opaqueIDAlphabet[n.Int64()]
	}
	str := string(b)
	if !opaqueIDPattern.MatchString(str) {
		return "", fmt.Errorf("generated opaqueID does not pass regex: %s", str)
	}
	return str, nil
}

var (
	ErrNotCreated = errors.New("no identity was created")
)

// A store is the backing store for hive identities.
type store struct {
	db *gorm.DB

	// template for creating *identity.Identity from a row value
	template idTemplate
}

type idTemplate func(handleInternal, opaqueID, signingPublicKey, kemPublicKey string) *identity.Identity

func newStore(db *gorm.DB, template idTemplate) (*store, error) {
	return &store{
		db:       db,
		template: template,
	}, nil
}

// persistIdentity writes a prepared ident row to the given DB (or transaction).
func (s *store) mintIdentity(ctx context.Context, handle string) (*identity.Identity, error) {
	opaqueID, err := generateOpaqueID()
	if err != nil {
		return nil, fmt.Errorf("generateOpaqueID: %w", err)
	}
	pubMultibase, privMultibase, err := generateSigningKeyPair()
	if err != nil {
		return nil, fmt.Errorf("generateSigningKeyPair: %w", err)
	}
	kemPubMultibase, kemPrivMultibase, err := generateSigningKeyPair()
	if err != nil {
		return nil, fmt.Errorf("generate kem key pair: %w", err)
	}
	row := &ident{
		Handle:               handle,
		OpaqueID:             opaqueID,
		SigningPublicKey:     pubMultibase,
		SigningPrivateKeyEnc: privMultibase, // TODO: encrypt before storing
		KEMPublicKey:         kemPubMultibase,
		KEMPrivateKeyEnc:     kemPrivMultibase, // TODO: encrypt before storing
	}
	result := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(row)
	if result.Error != nil {
		return nil, result.Error
	} else if result.RowsAffected == 0 {
		return nil, ErrNotCreated
	}
	return s.identity(row), nil
}

// getMemberByHandle fetches the member via handle (with member namespace stripped already) from the store
func (s *store) getIdentityByHandle(
	ctx context.Context,
	internalHandle string,
) (*identity.Identity, error) {
	var id ident
	result := s.db.WithContext(ctx).Where("handle = ?", internalHandle).First(&id)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, identity.ErrHandleNotFound
	}
	if result.Error != nil {
		return nil, result.Error
	}
	if err := s.ensureKEMKey(ctx, &id); err != nil {
		return nil, err
	}
	return s.identity(&id), nil
}

// getSigningPrivateKeyByID fetches and parses the signing private key for the identity
// with the given opaqueID. The private key is the atproto signing key registered in the
// identity's did:web doc, so it can be used to mint atproto-compatible service auth JWTs.
func (s *store) getSigningPrivateKeyByID(
	ctx context.Context,
	opaqueID string,
) (atcrypto.PrivateKey, error) {
	var id ident
	result := s.db.WithContext(ctx).Where("opaque_id = ?", opaqueID).First(&id)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, identity.ErrDIDNotFound
	}
	if result.Error != nil {
		return nil, result.Error
	}
	// TODO: decrypt SigningPrivateKeyEnc once we encrypt it at rest (see prepareIdentity).
	priv, err := atcrypto.ParsePrivateMultibase(id.SigningPrivateKeyEnc)
	if err != nil {
		return nil, fmt.Errorf("parsing stored signing private key: %w", err)
	}
	return priv, nil
}

// getMemberByDID fetches the member via opaque ID from the store
func (s *store) getIdentityByID(ctx context.Context, opaqueID string) (*identity.Identity, error) {

	var id ident
	result := s.db.WithContext(ctx).Where("opaque_id = ?", opaqueID).First(&id)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, identity.ErrDIDNotFound
	}
	if result.Error != nil {
		return nil, result.Error
	}
	if err := s.ensureKEMKey(ctx, &id); err != nil {
		return nil, err
	}
	return s.identity(&id), nil
}

// identity builds the DID document identity for a stored row.
func (s *store) identity(id *ident) *identity.Identity {
	return s.template(id.Handle, id.OpaqueID, id.SigningPublicKey, id.KEMPublicKey)
}

// ensureKEMKey backfills a KEM key for an identity minted before hive
// generated them, so its DID doc can publish #atproto_kem. Hive serves its
// identities' did:web documents itself, so storing the key is publishing it.
// The update only fills an empty key, so concurrent backfills of one row keep
// whichever key landed first, and the row is re-read to return that one.
func (s *store) ensureKEMKey(ctx context.Context, id *ident) error {
	if id.KEMPublicKey != "" {
		return nil
	}
	pub, priv, err := generateSigningKeyPair()
	if err != nil {
		return fmt.Errorf("generate kem key pair: %w", err)
	}
	if err := s.db.WithContext(ctx).Model(&ident{}).
		Where("opaque_id = ? AND (kem_public_key = '' OR kem_public_key IS NULL)", id.OpaqueID).
		Updates(map[string]any{
			"kem_public_key":      pub,
			"kem_private_key_enc": priv, // TODO: encrypt before storing
		}).Error; err != nil {
		return fmt.Errorf("backfill kem key: %w", err)
	}
	return s.db.WithContext(ctx).Where("opaque_id = ?", id.OpaqueID).First(id).Error
}

// getKEMPrivateKeyByID fetches and parses the KEM private key for the
// identity with the given opaqueID, backfilling one if it has none yet.
func (s *store) getKEMPrivateKeyByID(
	ctx context.Context,
	opaqueID string,
) (*atcrypto.PrivateKeyP256, error) {
	var id ident
	result := s.db.WithContext(ctx).Where("opaque_id = ?", opaqueID).First(&id)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, identity.ErrDIDNotFound
	}
	if result.Error != nil {
		return nil, result.Error
	}
	if err := s.ensureKEMKey(ctx, &id); err != nil {
		return nil, err
	}
	// TODO: decrypt KEMPrivateKeyEnc once we encrypt it at rest.
	priv, err := atcrypto.ParsePrivateMultibase(id.KEMPrivateKeyEnc)
	if err != nil {
		return nil, fmt.Errorf("parsing stored kem private key: %w", err)
	}
	kem, ok := priv.(*atcrypto.PrivateKeyP256)
	if !ok {
		return nil, errors.New("stored kem private key is not P-256")
	}
	return kem, nil
}

// Models returns the GORM models this package persists. Their tables are
// created by db.Migrate.
func Models() []any {
	return []any{&ident{}}
}
