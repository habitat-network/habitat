package hive

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"

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

// createMember generates all the necessary keys / ids for a DID identity + doc with this handle
func (s *store) createIdentity(handle string) (*identity.Identity, error) {
	opaqueID, err := generateOpaqueID()
	if err != nil {
		return nil, err
	}

	pubMultibase, privMultibase, err := generateSigningKeyPair()
	if err != nil {
		return nil, err
	}

	id := &ident{
		Handle:               handle,
		OpaqueID:             opaqueID,
		SigningPublicKey:     pubMultibase,
		SigningPrivateKeyEnc: privMultibase, // TODO: encrypt before storing
	}

	result := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(id)
	if result.Error != nil {
		return nil, result.Error
	} else if result.RowsAffected == 0 {
		// On conflict do nothing and surface the error if no row was created
		return nil, ErrNotCreated
	}

	return s.template(id.Handle, id.OpaqueID, id.SigningPublicKey), nil
}

// getMemberByHandle fetches the member via handle (with member namespace stripped already) from the store
func (s *store) getIdentityByHandle(internalHandle string) (*identity.Identity, error) {
	var id ident
	result := s.db.Where("handle = ?", internalHandle).First(&id)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, identity.ErrHandleNotFound
	}
	if result.Error != nil {
		return nil, result.Error
	}
	return s.template(id.Handle, id.OpaqueID, id.SigningPublicKey), nil
}

// getMemberByDID fetches the member via opaque ID from the store
func (s *store) getIdentityByID(opaqueID string) (*identity.Identity, error) {

	var id ident
	result := s.db.Where("opaque_id = ?", opaqueID).First(&id)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, identity.ErrDIDNotFound
	}
	if result.Error != nil {
		return nil, result.Error
	}
	return s.template(id.Handle, id.OpaqueID, id.SigningPublicKey), nil
}
