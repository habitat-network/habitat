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

type store struct {
	db *gorm.DB
}

func newStore(db *gorm.DB) (*store, error) {
	err := db.AutoMigrate(&ident{})
	if err != nil {
		return nil, err
	}
	return &store{db: db}, nil
}

// createMember generates all the necessary keys / ids for a DID identity + doc with this handle
func (s *store) createIdentity(handle string) error {
	opaqueID, err := generateOpaqueID()
	if err != nil {
		return err
	}

	pubMultibase, privMultibase, err := generateSigningKeyPair()
	if err != nil {
		return err
	}

	id := &ident{
		Handle:               handle,
		OpaqueID:             opaqueID,
		SigningPublicKey:     pubMultibase,
		SigningPrivateKeyEnc: privMultibase, // TODO: encrypt before storing
	}

	result := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(id)
	if result.Error != nil {
		return result.Error
	} else if result.RowsAffected == 0 {
		// On conflict do nothing and surface the error if no row was created
		return ErrNotCreated
	}

	return nil
}

// getMemberByHandle fetches the member via handle (with member namespace stripped already) from the store
func (s *store) getIdentityByHandle(handle string) (IdentPublic, error) {
	var id ident
	result := s.db.Where("handle = ?", handle).First(&id)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return IdentPublic{}, identity.ErrHandleNotFound
	}
	if result.Error != nil {
		return IdentPublic{}, result.Error
	}
	return IdentPublic{
		Handle:           id.Handle,
		OpaqueID:         id.OpaqueID,
		SigningPublicKey: id.SigningPublicKey,
	}, nil
}

// getMemberByDID fetches the member via opaque ID from the store
func (s *store) getIdentityByID(opaqueID string) (IdentPublic, error) {
	var id ident
	result := s.db.Where("opaque_id = ?", opaqueID).First(&id)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return IdentPublic{}, identity.ErrDIDNotFound
	}
	if result.Error != nil {
		return IdentPublic{}, result.Error
	}
	return IdentPublic{
		Handle:           id.Handle,
		OpaqueID:         id.OpaqueID,
		SigningPublicKey: id.SigningPublicKey,
	}, nil
}
