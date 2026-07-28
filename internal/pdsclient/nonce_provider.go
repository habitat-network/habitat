package pdsclient

import "sync"

// This simple implementation stores a single nonce value in memory.
// Must be thread-safe since it might be shared.
type MemoryNonceProvider struct {
	mu    sync.RWMutex
	nonce string
}

var _ DpopNonceProvider = (*MemoryNonceProvider)(nil)

// GetDpopNonce retrieves the current DPoP nonce.
// Returns the nonce value, whether a nonce is available, and any error.
func (n *MemoryNonceProvider) GetDpopNonce() (string, bool, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.nonce, n.nonce != "", nil
}

// SetDpopNonce stores a new DPoP nonce value.
// This is called when the server returns a new nonce in the DPoP-Nonce header.
func (n *MemoryNonceProvider) SetDpopNonce(nonce string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.nonce = nonce
	return nil
}
