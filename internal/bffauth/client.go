package bffauth

import "github.com/bluesky-social/indigo/atproto/crypto"

type ExternalHabitatUser struct {
	DID       string
	PublicKey crypto.PublicKey
	Host      string
}

type Client struct {
	// This is the store of temporary friends that are added to the BFF.
	// Helps us avoid implementing full public key resolution from DIDs with TailScale / localhost  setups
	tempFriendStore map[string]*ExternalHabitatUser
}

func (c *Client) GetToken(remoteHabitatUser *ExternalHabitatUser) (string, error) {
	// Stub - not implemented
	return "", nil
}
