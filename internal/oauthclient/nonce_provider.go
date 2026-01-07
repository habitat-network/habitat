package oauthclient

// This simple implementation stores a single nonce value in memory.
type MemoryNonceProvider struct{ nonce string }

var _ DpopNonceProvider = (*MemoryNonceProvider)(nil)

// GetDpopNonce retrieves the current DPoP nonce.
// Returns the nonce value, whether a nonce is available, and any error.
func (n *MemoryNonceProvider) GetDpopNonce() (string, bool, error) {
	return n.nonce, true, nil
}

// SetDpopNonce stores a new DPoP nonce value.
// This is called when the server returns a new nonce in the DPoP-Nonce header.
func (n *MemoryNonceProvider) SetDpopNonce(nonce string) error {
	n.nonce = nonce
	return nil
}
