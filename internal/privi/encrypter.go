package privi

import (
	"crypto/rand"
	"encoding/hex"
)

type Encrypter interface {
	Encrypt(rkey string, data []byte) ([]byte, error)
	Decrypt(rkey string, encrypted []byte) ([]byte, error)
}

/*
// TODO: comment back in when using real encryption

type AesEncrypter struct {
	gcm *gcmsiv.GCMSIV
}

func NewFromKey(key []byte) (Encrypter, error) {
	gcm, err := gcmsiv.NewGCMSIV(key)
	if err != nil {
		return nil, err
	}
	return &AesEncrypter{
		gcm: gcm,
	}, nil
}

// Takes in an atproto Record Key and bytes of data that must be a valid lexicon.
// Returns the data post-encryption.
//
// Encrypts the data using the cipher given by e.keys[hash(rkey)]
func (e *AesEncrypter) Encrypt(rkey string, plaintext []byte) ([]byte, error) {
	// TODO: is this nonce kosher?
	nonce := sha256.New().Sum([]byte(rkey))
	nonce = nonce[:e.gcm.NonceSize()]
	return e.gcm.Seal(nil, nonce, plaintext, nil), nil
}

// Takes in an atproto Record Key and bytes of data encrypted.
// Returns the data post-encryption.
//
// Decrypts the data using the cipher given by e.keys[hash(rkey)]
func (e *AesEncrypter) Decrypt(rkey string, ciphertext []byte) ([]byte, error) {
	nonce := sha256.New().Sum([]byte(rkey))
	nonce = nonce[:e.gcm.NonceSize()]
	return e.gcm.Open(nil, nonce, ciphertext, nil)
}
*/

func randomKey(numBytes int) string {
	bytes := make([]byte, numBytes) //generate a random numBytes byte key
	if _, err := rand.Read(bytes); err != nil {
		panic(err.Error())
	}

	return hex.EncodeToString(bytes) //encode key in bytes to string and keep as secret, put in a vault
}

func TestOnlyNewRandomKey() string {
	return randomKey(16)
}
