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

type idTemplate func(handleInternal, opaqueID, signingPublicKey string) *identity.Identity

func newStore(db *gorm.DB, template idTemplate) (*store, error) {
	err := db.AutoMigrate(&ident{})
	if err != nil {
		return nil, err
	}
	return &store{
		db:       db,
		template: template,
	}, nil
}

// prepareIdentity generates all keys and IDs for a new identity without writing to the DB.
// Returns the row to persist and the public identity.
func (s *store) prepareIdentity(handle string) (*ident, *identity.Identity, error) {
	opaqueID, err := generateOpaqueID()
	if err != nil {
		return nil, nil, err
	}

	pubMultibase, privMultibase, err := generateSigningKeyPair()
	if err != nil {
		return nil, nil, err
	}

	row := &ident{
		Handle:               handle,
		OpaqueID:             opaqueID,
		SigningPublicKey:     pubMultibase,
		SigningPrivateKeyEnc: privMultibase, // TODO: encrypt before storing
	}
	return row, s.template(row.Handle, row.OpaqueID, row.SigningPublicKey), nil
}

// persistIdentity writes a prepared ident row to the given DB (or transaction).
func persistIdentity(tx *gorm.DB, row *ident) error {
	result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(row)
	if result.Error != nil {
		return result.Error
	} else if result.RowsAffected == 0 {
		return ErrNotCreated
	}
	return nil
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
	return s.template(id.Handle, id.OpaqueID, id.SigningPublicKey), nil
}

// getSigningPrivateKeyByID fetches and parses the signing private key for the identity
// with the given opaqueID. The private key is the atproto signing key registered in the
// identity's did:web doc, so it can be used to mint atproto-compatible service auth JWTs.
func (s *store) getSigningPrivateKeyByID(ctx context.Context, opaqueID string) (atcrypto.PrivateKey, error) {
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
	return s.template(id.Handle, id.OpaqueID, id.SigningPublicKey), nil
}
