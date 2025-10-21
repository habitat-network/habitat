package bffauth

import "github.com/bluesky-social/indigo/atproto/atcrypto"

type ExternalHabitatUser struct {
	DID       string
	PublicKey atcrypto.PublicKey
	Host      string
}

type Client interface {
	GetToken(did string) (string, error)
}

type client struct {
	// This is the store of temporary friends that are added to the BFF.
	// Helps us avoid implementing full public key resolution from DIDs with TailScale / localhost  setups
	// tempFriendStore map[string]*ExternalHabitatUser
}

func NewClient() Client {
	return &client{}
}

func (c *client) GetToken(did string) (string, error) {
	// Stub - not implemented
	return "", nil
}
