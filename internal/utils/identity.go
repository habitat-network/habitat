package utils

import "github.com/bluesky-social/indigo/atproto/identity"

func SpaceHostEndpoint(ident *identity.Identity) string {
	if spaceHost := ident.GetServiceEndpoint("atproto_space_host"); spaceHost != "" {
		return spaceHost
	}
	return ident.PDSEndpoint()
}
