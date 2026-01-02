package encrypt_test

import (
	"crypto/rand"
	"testing"

	"github.com/eagraf/habitat-new/internal/encrypt"
	"github.com/stretchr/testify/require"
)

func TestEncryptDecryptCBOR_RoundTrip(t *testing.T) {
	type TestStruct struct {
		Name  string
		Value int
		Items []string
	}

	original := TestStruct{
		Name:  "test",
		Value: 42,
		Items: []string{"foo", "bar", "baz"},
	}

	// Encrypt
	encrypted, err := encrypt.EncryptCBOR(original, encrypt.TestKey)
	require.NoError(t, err)
	require.NotEmpty(t, encrypted)

	// Decrypt
	var decrypted TestStruct
	err = encrypt.DecryptCBOR(encrypted, encrypt.TestKey, &decrypted)
	require.NoError(t, err)
	require.Equal(t, original, decrypted)
}

func TestEncryptCBOR_InvalidKeySize(t *testing.T) {
	tests := []struct {
		name    string
		keySize int
	}{
		{"too short", 16},
		{"too long", 64},
		{"empty", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := make([]byte, tt.keySize)
			_, err := encrypt.EncryptCBOR("test", key)
			require.Error(t, err)
			require.Contains(t, err.Error(), "encryption key must be exactly 32 bytes")
		})
	}
}

func TestDecryptCBOR_InvalidKeySize(t *testing.T) {
	// Create a valid encrypted token
	encrypted, err := encrypt.EncryptCBOR("test", encrypt.TestKey)
	require.NoError(t, err)

	tests := []struct {
		name    string
		keySize int
	}{
		{"too short", 16},
		{"too long", 64},
		{"empty", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			badKey := make([]byte, tt.keySize)
			var result string
			err := encrypt.DecryptCBOR(encrypted, badKey, &result)
			require.Error(t, err)
			require.Contains(t, err.Error(), "encryption key must be exactly 32 bytes")
		})
	}
}

func TestDecryptCBOR_WrongKey(t *testing.T) {
	differentKey := []byte("different-key-32-098765432109876")

	// Encrypt with key1
	encrypted, err := encrypt.EncryptCBOR("secret data", encrypt.TestKey)
	require.NoError(t, err)

	// Try to decrypt with key2
	var result string
	err = encrypt.DecryptCBOR(encrypted, differentKey, &result)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid token")
}

func TestDecryptCBOR_InvalidBase64(t *testing.T) {
	var result string
	err := encrypt.DecryptCBOR("not-valid-base64!@#$", encrypt.TestKey, &result)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid token")
}

func TestDecryptCBOR_TruncatedData(t *testing.T) {
	// Create a valid encrypted token
	encrypted, err := encrypt.EncryptCBOR("test", encrypt.TestKey)
	require.NoError(t, err)

	// Truncate it (remove some characters)
	truncated := encrypted[:len(encrypted)/2]

	var result string
	err = encrypt.DecryptCBOR(truncated, encrypt.TestKey, &result)
	require.Error(t, err)
}

func TestDecryptCBOR_CorruptedData(t *testing.T) {
	// Create a valid encrypted token
	encrypted, err := encrypt.EncryptCBOR("test", encrypt.TestKey)
	require.NoError(t, err)

	// Corrupt the data by replacing a character
	corrupted := encrypted[:len(encrypted)-5] + "XXXXX"

	var result string
	err = encrypt.DecryptCBOR(corrupted, encrypt.TestKey, &result)
	require.Error(t, err)
}

func TestEncryptDecryptCBOR_DifferentTypes(t *testing.T) {
	tests := []struct {
		name string
		data any
	}{
		{
			name: "string",
			data: "hello world",
		},
		{
			name: "int",
			data: 42,
		},
		{
			name: "float",
			data: 3.14,
		},
		{
			name: "bool",
			data: true,
		},
		{
			name: "slice",
			data: []string{"a", "b", "c"},
		},
		{
			name: "map",
			data: map[string]int{"foo": 1, "bar": 2},
		},
		{
			name: "empty string",
			data: "",
		},
		{
			name: "zero",
			data: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encrypted, err := encrypt.EncryptCBOR(tt.data, encrypt.TestKey)
			require.NoError(t, err)
			require.NotEmpty(t, encrypted)

			// Use type assertion to create the right type for decryption
			switch v := tt.data.(type) {
			case string:
				var result string
				err = encrypt.DecryptCBOR(encrypted, encrypt.TestKey, &result)
				require.NoError(t, err)
				require.Equal(t, v, result)
			case int:
				var result int
				err = encrypt.DecryptCBOR(encrypted, encrypt.TestKey, &result)
				require.NoError(t, err)
				require.Equal(t, v, result)
			case float64:
				var result float64
				err = encrypt.DecryptCBOR(encrypted, encrypt.TestKey, &result)
				require.NoError(t, err)
				require.Equal(t, v, result)
			case bool:
				var result bool
				err = encrypt.DecryptCBOR(encrypted, encrypt.TestKey, &result)
				require.NoError(t, err)
				require.Equal(t, v, result)
			case []string:
				var result []string
				err = encrypt.DecryptCBOR(encrypted, encrypt.TestKey, &result)
				require.NoError(t, err)
				require.Equal(t, v, result)
			case map[string]int:
				var result map[string]int
				err = encrypt.DecryptCBOR(encrypted, encrypt.TestKey, &result)
				require.NoError(t, err)
				require.Equal(t, v, result)
			}
		})
	}
}

func TestDecryptCBOR_NilDataParameter(t *testing.T) {
	// Encrypt some data
	encrypted, err := encrypt.EncryptCBOR("test data", encrypt.TestKey)
	require.NoError(t, err)

	// Decrypt with nil data parameter (should not error, just won't decode)
	err = encrypt.DecryptCBOR(encrypted, encrypt.TestKey, nil)
	require.NoError(t, err)
}

func TestEncryptCBOR_UniqueNonces(t *testing.T) {
	// Encrypt the same data multiple times
	data := "same data every time"
	encrypted1, err := encrypt.EncryptCBOR(data, encrypt.TestKey)
	require.NoError(t, err)

	encrypted2, err := encrypt.EncryptCBOR(data, encrypt.TestKey)
	require.NoError(t, err)

	encrypted3, err := encrypt.EncryptCBOR(data, encrypt.TestKey)
	require.NoError(t, err)

	// All encrypted values should be different (due to unique nonces)
	require.NotEqual(t, encrypted1, encrypted2)
	require.NotEqual(t, encrypted2, encrypted3)
	require.NotEqual(t, encrypted1, encrypted3)

	// But all should decrypt to the same value
	var result1, result2, result3 string
	require.NoError(t, encrypt.DecryptCBOR(encrypted1, encrypt.TestKey, &result1))
	require.NoError(t, encrypt.DecryptCBOR(encrypted2, encrypt.TestKey, &result2))
	require.NoError(t, encrypt.DecryptCBOR(encrypted3, encrypt.TestKey, &result3))

	require.Equal(t, data, result1)
	require.Equal(t, data, result2)
	require.Equal(t, data, result3)
}

func TestEncryptDecryptCBOR_ComplexStruct(t *testing.T) {
	type Address struct {
		Street  string
		City    string
		ZipCode string
	}

	type Person struct {
		Name      string
		Age       int
		Email     string
		Active    bool
		Scores    []float64
		Metadata  map[string]string
		Address   Address
		Addresses []Address
	}

	original := Person{
		Name:   "John Doe",
		Age:    30,
		Email:  "john@example.com",
		Active: true,
		Scores: []float64{95.5, 87.3, 92.1},
		Metadata: map[string]string{
			"role":       "admin",
			"department": "engineering",
		},
		Address: Address{
			Street:  "123 Main St",
			City:    "San Francisco",
			ZipCode: "94102",
		},
		Addresses: []Address{
			{Street: "456 Oak Ave", City: "Oakland", ZipCode: "94601"},
			{Street: "789 Pine Rd", City: "Berkeley", ZipCode: "94704"},
		},
	}

	// Encrypt
	encrypted, err := encrypt.EncryptCBOR(original, encrypt.TestKey)
	require.NoError(t, err)
	require.NotEmpty(t, encrypted)

	// Decrypt
	var decrypted Person
	err = encrypt.DecryptCBOR(encrypted, encrypt.TestKey, &decrypted)
	require.NoError(t, err)
	require.Equal(t, original, decrypted)
}

func TestEncryptCBOR_RandomKey(t *testing.T) {
	// Test with a truly random key
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)

	data := "test data with random key"

	encrypted, err := encrypt.EncryptCBOR(data, key)
	require.NoError(t, err)

	var decrypted string
	err = encrypt.DecryptCBOR(encrypted, key, &decrypted)
	require.NoError(t, err)
	require.Equal(t, data, decrypted)
}

func TestEncryptCBOR_LargeData(t *testing.T) {
	// Create a large slice
	largeSlice := make([]string, 1000)
	for i := range largeSlice {
		largeSlice[i] = "item-" + string(rune(i))
	}

	encrypted, err := encrypt.EncryptCBOR(largeSlice, encrypt.TestKey)
	require.NoError(t, err)

	var decrypted []string
	err = encrypt.DecryptCBOR(encrypted, encrypt.TestKey, &decrypted)
	require.NoError(t, err)
	require.Equal(t, largeSlice, decrypted)
}

func TestParseKey(t *testing.T) {
	key, error := encrypt.GenerateKey()
	require.NoError(t, error)

	parsedKey, err := encrypt.ParseKey(key)
	require.NoError(t, err)
	require.Equal(t, 32, len(parsedKey))
}
