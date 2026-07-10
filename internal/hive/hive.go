package hive

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	_ "github.com/bluesky-social/indigo/atproto/auth" // register signing methods
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/habitat-network/habitat/internal/db"
	"gorm.io/gorm"
)

var handlePattern = regexp.MustCompile(`^[a-zA-Z0-9]{1,50}$`)

// hive roughly stands for "habitat identity verification and enrollment" and it is an identity
// service for habitat organizations.
// This package serves org identities based on the DID spec and the did:web: method to be legible to and interoperable
// with the broader AT protocol network, but allow orgs to own the user identities and management and
// not rely on the PLC directory as a central source of failure.

type Hive interface {
	MintOrgIdentity(ctx context.Context, subdomain string) (*identity.Identity, error)
	// Minting new identities for members
	MintIdentity(ctx context.Context, handle string, subdomain string) (*identity.Identity, error)
	// SignServiceJWT mints a signed JWT using the did's signing key
	SignJWT(
		ctx context.Context,
		did syntax.DID,
		headers map[string]any,
		claims jwt.Claims,
	) (string, error)
	// FUTURE METHODS:
	// Updating a handle
	// UpdateHandle(ctx context.Context, did string, oldHandle string, newHandle string)
	// Rotate signing key
	// Deactivate user

	// Implements the same interface as the PLC / any identity Directory in atproto land
	identity.Directory
	db.Store[Hive]
}

type hive struct {
	// member namespace / subdomain where identities are created
	// "members" in alice.members.sf.club and "" in alice.sf.club
	memberDomain string

	// The domain at which identity's pear is hosted at (what goes in the #habitat) service in DID doc
	pearDomain string

	// The backing data store for hive
	store *store
}

var _ Hive = &hive{}

// toIdentity builds an identity.Identity from a stored IdentPublic and its known DID.
func idTemplateBuilder(memberDomain, pearDomain string) idTemplate {
	return func(handleInternal, opaqueID, signingPublicKey string) *identity.Identity {
		handle := syntax.Handle(handleInternal + "." + memberDomain)
		DID := syntax.DID("did:web:" + opaqueID + "." + memberDomain)
		return &identity.Identity{
			DID:         DID,
			Handle:      handle,
			AlsoKnownAs: []string{"at://" + string(handle)},
			Keys: map[string]identity.VerificationMethod{
				"atproto": {
					Type:               "Multikey",
					PublicKeyMultibase: signingPublicKey,
				},
			},
			Services: map[string]identity.ServiceEndpoint{
				"habitat": {
					Type: "HabitatServer",
					URL:  "https://" + pearDomain,
				},
			},
		}
	}
}

func NewHive(memberDomain string, pearDomain string, db *gorm.DB) (Hive, error) {
	template := idTemplateBuilder(memberDomain, pearDomain)
	store, err := newStore(db, template)
	if err != nil {
		return nil, err
	}
	h := &hive{
		memberDomain: memberDomain,
		pearDomain:   pearDomain,
		store:        store,
		// TODO: add a cache directory here
	}
	return h, nil
}

// Lookup implements identity.Directory
func (h *hive) Lookup(ctx context.Context, atid syntax.AtIdentifier) (*identity.Identity, error) {
	if atid.IsDID() {
		did, err := atid.AsDID()
		if err != nil {
			return nil, err
		}
		return h.LookupDID(ctx, did)
	}
	handle, err := atid.AsHandle()
	if err != nil {
		return nil, err
	}
	return h.LookupHandle(ctx, handle)
}

// LookupDID implements identity.Directory
func (h *hive) LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error) {
	content := strings.TrimPrefix(did.String(), "did:web:")
	opaqueID, found := strings.CutSuffix(content, "."+h.memberDomain)
	if !found {
		return nil, identity.ErrDIDNotFound
	}
	return h.store.getIdentityByID(ctx, opaqueID)
}

// LookupHandle implements identity.Directory
// It strips the member domain suffix from the given handle. Handle format is <internal-handle>.<memberDomain>
// (e.g. "admin.acmecorp2" for org subdomain handles).
func (h *hive) LookupHandle(ctx context.Context, handle syntax.Handle) (*identity.Identity, error) {
	internalHandle, found := strings.CutSuffix(handle.String(), "."+h.memberDomain)
	if !found {
		return nil, identity.ErrHandleNotFound
	}
	return h.store.getIdentityByHandle(ctx, internalHandle)
}

// Purge implements identity.Directory
func (h *hive) Purge(ctx context.Context, atid syntax.AtIdentifier) error {
	// Empty because we're not caching anything
	return nil
}

// SignJWT implements Hive.
func (h *hive) SignJWT(
	ctx context.Context,
	did syntax.DID,
	headers map[string]any,
	claims jwt.Claims,
) (string, error) {
	// Validate the DID belongs to this hive and extract the opaque ID.
	content := strings.TrimPrefix(did.String(), "did:web:")
	opaqueID, after, ok := strings.Cut(content, ".")
	if !ok || after != h.memberDomain {
		return "", identity.ErrDIDNotFound
	}
	priv, err := h.store.getSigningPrivateKeyByID(ctx, opaqueID)
	if err != nil {
		return "", fmt.Errorf("failed to get signing private key: %w", err)
	}
	method := jwt.GetSigningMethod("ES256K")
	token := &jwt.Token{
		Header: headers,
		Claims: claims,
		Method: method,
	}
	if token.Header["typ"] == "" {
		token.Header["typ"] = "JWT"
	}
	token.Header["alg"] = "ES256K"
	tokenString, err := token.SignedString(priv)
	if err != nil {
		return "", fmt.Errorf("failed to sign JWT: %w", err)
	}
	return tokenString, nil
}

// MintOrgIdentity implements [Hive].
func (h *hive) MintOrgIdentity(ctx context.Context, subdomain string) (*identity.Identity, error) {
	return h.store.mintIdentity(ctx, subdomain)
}

// MintIdentity implements Hive.
func (h *hive) MintIdentity(
	ctx context.Context,
	handlePrefix string,
	subdomain string,
) (*identity.Identity, error) {
	// Ensure handle passes regex
	if !handlePattern.MatchString(handlePrefix) {
		return nil, identity.ErrInvalidHandle
	}
	fullHandle := handlePrefix + "." + subdomain
	return h.store.mintIdentity(ctx, fullHandle)
}

func (h *hive) WithTx(tx *gorm.DB) Hive {
	return &hive{
		memberDomain: h.memberDomain,
		pearDomain:   h.pearDomain,
		store:        &store{db: tx, template: h.store.template},
	}
}
