package hive

import (
	"regexp"
	"time"
)

// Database models for hive / identities

// hive is served by an org backend, for example at habitat.collectiveaction.school or my-community.habitat.network.
// Each org has a namespace for member IDs, like arushi.my-community.habitat.network
// Member identities for orgs are minted and served by hive. These are the backing data models.

// An example DID doc looks like this for an org that is self-hosting habitat
/*
{
  "@context": [
    "https://www.w3.org/ns/did/v1",
    "https://w3id.org/security/multikey/v1"
  ],
  "id": "did:web:a3f9k2.sf.club",
  "alsoKnownAs": ["at://alice.sf.club"],
  "verificationMethod": [{
    "id": "did:web:a3f9k2.sf.club#atproto",
    "type": "Multikey",
    "controller": "did:web:a3f9k2.sf.club",
    "publicKeyMultibase": "zQ3sh..."
  }],
  "service": [{
    "id": "#atproto_pds",
    "type": "AtprotoPersonalDataServer",
    "serviceEndpoint": "https://habitat.sf.club"
  }]
}
*/

// Or like this for a habitat-hosted org:
/*
{
  "@context": [
    "https://www.w3.org/ns/did/v1",
    "https://w3id.org/security/multikey/v1"
  ],
  "id": "did:web:a3f9k2.myorg.habitat.network",
  "alsoKnownAs": ["at://alice.myorg.habitat.network"],
  "verificationMethod": [{
    "id": "did:web:a3f9k2.myorg.habitat.network#atproto",
    "type": "Multikey",
    "controller": "did:web:a3f9k2.myorg.habitat.network",
    "publicKeyMultibase": "zQ3sh..."
  }],
  "service": [{
    "id": "#atproto_pds",
    "type": "AtprotoPersonalDataServer",
    "serviceEndpoint": "https://myorg.habitat.network"
  }]
}
*/

// In a table view it looks like this:
// DID:              did:web:a3f9k2.myorg.habitat.network   |   did:web:a3f9k2.sf.club
// Handle:           alice.myorg.habitat.network             |   alice.sf.club
// Service endpoint: https://myorg.habitat.network           |   https://habitat.sf.club

// We have opaque DIDs so handle / DID are not confused in usage and it's clear that handles are for 'what people identify me as'
// The opaqueIDs must match the following pattern:
var opaqueIDPattern = regexp.MustCompile(`^[a-z0-9]{6}$`)

type ident struct {
	// Identifiers needed in DID doc
	Handle   string `gorm:"primaryKey"`
	OpaqueID string `gorm:"uniqueIndex"`

	// Key management
	SigningPublicKey     string
	SigningPrivateKeyEnc string

	// Automatically managed by gorm
	CreatedAt time.Time
	UpdatedAt time.Time
}
