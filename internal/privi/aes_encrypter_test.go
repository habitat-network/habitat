package privi

/*
// TODO: comment back in when using real encryption

func TestAesEncrypter(t *testing.T) {
	e, err := NewFromKey([]byte(randomKey(16)))
	require.NoError(t, err)

	rkey := "my-rkey"
	data := []byte("this is my data lalalala")

	// Make sure decrypt(encrypted) == encrypt(decrypted)
	enc, err := e.Encrypt(rkey, data)
	require.NoError(t, err)
	dec, err := e.Decrypt(rkey, enc)
	require.NoError(t, err)
	require.Equal(t, dec, data)
}
*/
