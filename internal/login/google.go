package login

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// GoogleEmailMappingStore resolves an org DID to the email address used
// for Google Sign-In.
type GoogleEmailMappingStore interface {
	GetEmail(ctx context.Context, orgDID syntax.DID) (string, error)
}

type googleProvider struct {
	loginMethod  string
	mappingStore GoogleEmailMappingStore
}

func NewGoogleProvider(mappingStore GoogleEmailMappingStore) Provider {
	return &googleProvider{
		loginMethod:  "google",
		mappingStore: mappingStore,
	}
}

func (p *googleProvider) LoginMethod() string { return p.loginMethod }

func (p *googleProvider) Authorize(ctx context.Context, id *identity.Identity) (string, []byte, error) {
	email, err := p.mappingStore.GetEmail(ctx, id.DID)
	if err != nil {
		return "", nil, fmt.Errorf("lookup google email for org %s: %w", id.DID, err)
	}
	if email == "" {
		return "", nil, fmt.Errorf("no google email configured for org %s", id.DID)
	}
	// TODO: implement Google OAuth initiation with login_hint=email
	_ = email
	return "", nil, fmt.Errorf("google provider not yet implemented")
}

func (p *googleProvider) Exchange(_ context.Context, _ syntax.DID, _, _ string, _ []byte) error {
	// TODO: implement Google OAuth callback handling
	return fmt.Errorf("google provider not yet implemented")
}
