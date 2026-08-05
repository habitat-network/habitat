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
	handle   syntax.Handle
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

// Handle sets the identity's handle.
func (b *Builder) Handle(handle syntax.Handle) *Builder {
	b.handle = handle
	return b
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

func (b *Builder) ATProtoSpaceKey(multibase string) *Builder {
	return b.VerificationMethod(
		fmt.Sprintf("%s#atproto_space", b.id),
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

func (b *Builder) ATProtoSpaceHost(endpoint string) *Builder {
	return b.Service("#atproto_space_host", "AtprotoSpaceHost", endpoint)
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

// Build assembles the identity.
func (b *Builder) Build() *identity.Identity {
	keys := make(map[string]identity.VerificationMethod, len(b.methods))
	for _, m := range b.methods {
		_, frag, ok := strings.Cut(m.ID, "#")
		if !ok {
			continue
		}
		keys[frag] = identity.VerificationMethod{
			Type:               m.Type,
			PublicKeyMultibase: m.PublicKeyMultibase,
		}
	}
	services := make(map[string]identity.ServiceEndpoint, len(b.services))
	for _, s := range b.services {
		_, frag, ok := strings.Cut(s.ID, "#")
		if !ok {
			continue
		}
		services[frag] = identity.ServiceEndpoint{
			Type: s.Type,
			URL:  s.ServiceEndpoint,
		}
	}
	return &identity.Identity{
		DID:         b.id,
		Handle:      b.handle,
		AlsoKnownAs: b.aka,
		Keys:        keys,
		Services:    services,
	}
}
