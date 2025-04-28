package privi

// NoopEncrypter implements Encrypter but it doesn't actually do encryption.
type NoopEncrypter struct{}

func (e *NoopEncrypter) Encrypt(rkey string, data []byte) ([]byte, error) {
	return data, nil
}
func (e *NoopEncrypter) Decrypt(rkey string, encrypted []byte) ([]byte, error) {
	return encrypted, nil
}
