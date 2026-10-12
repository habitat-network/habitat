package sap

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/did"
)

const (
	// DefaultServiceName is the DID-document service fragment sap names itself
	// by when Config.ServiceID is empty. Deployments that already publish a
	// service under a different name (e.g. the permissioned-data proposal's
	// `atproto_space_syncer`) set Config.ServiceID to it.
	DefaultServiceName = "habitat_space_syncer"
)

// ServiceIdentity is how sap names itself to space hosts: the did:web DID it
// serves its DID document under, and the service fragment within that document
// that receive endpoints hang off.
type ServiceIdentity struct {
	// DID is sap's own did:web DID, e.g. did:web:sap.example.com.
	DID syntax.DID
	// Name is the service fragment, e.g. habitat_space_syncer.
	Name string
}

// Ref is the full service identifier sap registers with space hosts, e.g.
// "did:web:sap.example.com#habitat_space_syncer". This is what deliveries are
// addressed to, and what space hosts resolve to find sap's endpoint.
func (s ServiceIdentity) Ref() string {
	return s.DID.String() + "#" + s.Name
}

// NewServiceIdentity derives sap's service identity from its public base URL
// and the service name to publish it under. An empty serviceName falls back to
// DefaultServiceName.
//
// The endpoint's host must still be one a did:web resolver can fetch. did.Web
// percent-encodes a port, per the spec, but the reference resolver additionally
// requires a plain hostname with a real TLD — so a port-bearing or .localhost
// endpoint produces a well-formed DID that will not resolve for a space host.
func NewServiceIdentity(endpoint, serviceName string) (ServiceIdentity, error) {
	if serviceName == "" {
		serviceName = DefaultServiceName
	}
	// A fragment is the only thing separating the DID from the service name in
	// the identifier, so anything that would blur that boundary has to go.
	if strings.ContainsAny(serviceName, "#") || strings.ContainsFunc(
		serviceName, unicode.IsSpace,
	) {
		return ServiceIdentity{}, fmt.Errorf(
			"invalid service name %q: must not contain '#' or whitespace", serviceName,
		)
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		return ServiceIdentity{}, fmt.Errorf("parse endpoint %q: %w", endpoint, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ServiceIdentity{}, fmt.Errorf(
			"endpoint %q must be an absolute http(s) URL", endpoint,
		)
	}
	// did.Web derives the DID the same way every other habitat did:web identity
	// does, including percent-encoding a port per the did:web spec.
	ident := did.Web(u.Host).Build()
	return ServiceIdentity{DID: ident.DID, Name: serviceName}, nil
}
