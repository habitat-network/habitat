package main

import "fmt"

// serviceID is the fragment of greensky's did:web service entry. The frontend
// targets it with an `Atproto-Proxy: <did>#greensky` header, and pear resolves
// it from the DID document to find where to forward the request.
const serviceID = "greensky"

// didConfig captures greensky's did:web identity, derived from its domain.
type didConfig struct {
	domain          string
	did             string
	serviceEndpoint string
}

func newDIDConfig(domain string) didConfig {
	return didConfig{
		domain:          domain,
		did:             "did:web:" + domain,
		serviceEndpoint: "https://" + domain,
	}
}

// didDocument is greensky's did:web document. pear resolves did:web:<domain> by
// fetching /.well-known/did.json and reads the #greensky service endpoint to
// forward network.habitat.greensky.* calls here.
func (c didConfig) didDocument() map[string]any {
	return map[string]any{
		"@context": []string{"https://www.w3.org/ns/did/v1"},
		"id":       c.did,
		"service": []map[string]any{
			{
				"id":              fmt.Sprintf("#%s", serviceID),
				"type":            "HabitatGreenskyServer",
				"serviceEndpoint": c.serviceEndpoint,
			},
		},
	}
}
