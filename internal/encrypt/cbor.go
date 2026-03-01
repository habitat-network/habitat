package encrypt

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/fxamacker/cbor/v2"
	"golang.org/x/crypto/nacl/secretbox"
)

var TestKey = []byte("test-encryption-key32-0123456789")

func EncryptCBOR(data any, key []byte) (string, error) {
	if len(key) != 32 {
		return "", fmt.Errorf("encryption key must be exactly 32 bytes, got %d", len(key))
	}
	var b bytes.Buffer
	if err := cbor.NewEncoder(&b).Encode(data); err != nil {
		return "", fmt.Errorf("failed to encode data: %w", err)
	}
	var nonce [24]byte
	_, err := io.ReadFull(rand.Reader, nonce[:])
	if err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}
	var keyBytes [32]byte
	copy(keyBytes[:], key)
	return base64.RawURLEncoding.EncodeToString(
		secretbox.Seal(nonce[:], b.Bytes(), &nonce, &keyBytes),
	), nil
}

func DecryptCBOR(token string, key []byte, data any) error {
	if len(key) != 32 {
		return fmt.Errorf("encryption key must be exactly 32 bytes, got %d", len(key))
	}
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return fmt.Errorf("invalid token: %w", err)
	}
	if len(b) < 24 {
		return fmt.Errorf("invalid token: data too short")
	}
	var nonce [24]byte
	copy(nonce[:], b[:24])
	var keyBytes [32]byte
	copy(keyBytes[:], key)
	decrypted, ok := secretbox.Open(nil, b[24:], &nonce, &keyBytes)
	if !ok {
		return fmt.Errorf("invalid token")
	}
	if data != nil {
		return cbor.NewDecoder(bytes.NewReader(decrypted)).Decode(data)
	}
	return nil
}

func ParseKey(key string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return nil, fmt.Errorf("failed to decode encryption key: %w", err)
	}
	if len(decoded) != 32 {
		return nil, fmt.Errorf("encryption key must be exactly 32 bytes, got %d", len(decoded))
	}
	return decoded, nil
}

func GenerateKey() (string, error) {
	key := make([]byte, 32)
	_, err := io.ReadFull(rand.Reader, key)
	if err != nil {
		return "", fmt.Errorf("failed to generate key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}
