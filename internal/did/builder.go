package did

import (
	"fmt"
	"strings"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// Builder accumulates verification methods and services for a DID document.
type Builder struct {
	id       syntax.DID
	aka      []string
	methods  []identity.DocVerificationMethod
	services []identity.DocService
}

// New returns a builder for an arbitrary DID.
func New(id syntax.DID) *Builder {
	return &Builder{id: id}
}

// Web returns a builder for a did:web identifier from a host. Ports are
// percent-encoded per the did:web spec (example.com:8443 -> did:web:example.com%3A8443).
func Web(host string) *Builder {
	didStr := "did:web:" + host
	if strings.Contains(host, ":") {
		parts := strings.SplitN(host, ":", 2)
		if len(parts) == 2 {
			didStr = fmt.Sprintf("did:web:%s%%3A%s", parts[0], parts[1])
		}
	}
	return New(syntax.DID(didStr))
}

// AlsoKnownAs appends alsoKnownAs URIs (e.g. "at://handle.example.com").
func (b *Builder) AlsoKnownAs(uris ...string) *Builder {
	b.aka = append(b.aka, uris...)
	return b
}

// AtprotoKey registers the atproto repo signing key as a Multikey verification
// method at <did>#atproto.
func (b *Builder) AtprotoKey(multibase string) *Builder {
	return b.VerificationMethod(
		fmt.Sprintf("%s#atproto", b.id),
		"Multikey",
		b.id.String(),
		multibase,
	)
}

// HabitatKey registers the host signing key as a Multikey verification method
// at <did>#habitat.
func (b *Builder) HabitatKey(multibase string) *Builder {
	return b.VerificationMethod(
		fmt.Sprintf("%s#habitat", b.id),
		"Multikey",
		b.id.String(),
		multibase,
	)
}

// Habitat adds the #habitat HabitatServer service.
func (b *Builder) Habitat(endpoint string) *Builder {
	return b.Service("#habitat", "HabitatServer", endpoint)
}

// ATProtoPDS adds the #atproto_pds AtprotoPersonalDataServer service.
func (b *Builder) ATProtoPDS(endpoint string) *Builder {
	return b.Service("#atproto_pds", "AtprotoPersonalDataServer", endpoint)
}

// VerificationMethod adds a custom verification method.
func (b *Builder) VerificationMethod(id, typ, controller, multibase string) *Builder {
	b.methods = append(b.methods, identity.DocVerificationMethod{
		ID:                 id,
		Type:               typ,
		Controller:         controller,
		PublicKeyMultibase: multibase,
	})
	return b
}

// Service adds a custom service.
func (b *Builder) Service(id, typ, endpoint string) *Builder {
	b.services = append(b.services, identity.DocService{
		ID:              id,
		Type:            typ,
		ServiceEndpoint: endpoint,
	})
	return b
}

// Build assembles the DID document.
func (b *Builder) Build() identity.DIDDocument {
	return identity.DIDDocument{
		DID:                b.id,
		AlsoKnownAs:        b.aka,
		VerificationMethod: b.methods,
		Service:            b.services,
	}
}
