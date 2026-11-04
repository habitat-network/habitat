package authn

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

var (
	// ErrInvalidDelegationToken means a delegation token is malformed, badly
	// signed, expired, or for another space. getSpaceCredential answers it
	// with InvalidDelegationToken.
	ErrInvalidDelegationToken = errors.New("invalid delegation token")
	// ErrUserNotAuthorized means a valid delegation token's issuer can't read
	// the space. getSpaceCredential answers it with UserNotAuthorized.
	ErrUserNotAuthorized = errors.New("user not authorized")
)

// DelegationTokenVerifier verifies the delegation tokens getSpaceCredential
// takes in its request body: a user's (or this host's, for #habitat) signed
// statement that it is acting for the space's credential request.
type DelegationTokenVerifier struct {
	dir     identity.Directory
	hostKey atcrypto.PrivateKey
	srv     SpaceRoleValidator
}

func NewDelegationTokenVerifier(
	directory identity.Directory,
	srv SpaceRoleValidator,
	hostKey atcrypto.PrivateKey,
) *DelegationTokenVerifier {
	return &DelegationTokenVerifier{
		dir:     directory,
		hostKey: hostKey,
		srv:     srv,
	}
}

// Verify checks token is a valid delegation token for space whose issuer can
// read space, and returns the issuer. It fails with ErrInvalidDelegationToken
// or ErrUserNotAuthorized, or another error if the role check itself fails.
func (d *DelegationTokenVerifier) Verify(
	ctx context.Context,
	token string,
	space habitat_syntax.SpaceURI,
) (syntax.DID, error) {
	parsed, err := jwt.ParseWithClaims(
		token,
		jwt.MapClaims{},
		fetchIssuerKeyFunc(ctx, d.dir, d.hostKey),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(time.Second*10),
	)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidDelegationToken, err)
	}
	if typ := parsed.Header["typ"]; typ != "atproto-space-delegation+jwt" {
		return "", fmt.Errorf("%w: unexpected typ %v", ErrInvalidDelegationToken, typ)
	}
	subj, err := getSpaceSubj(parsed.Claims)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidDelegationToken, err)
	}
	if subj != space {
		return "", fmt.Errorf("%w: token is for another space", ErrInvalidDelegationToken)
	}
	issuer, _ /* issuer must exist from verification */ := parsed.Claims.GetIssuer()
	allowed, err := d.srv.CheckUserHasSpaceRole(
		ctx,
		syntax.DID(issuer),
		space,
		habitat_syntax.SpaceRoleReader,
	)
	if err != nil {
		return "", fmt.Errorf("check space role: %w", err)
	}
	if !allowed {
		return "", ErrUserNotAuthorized
	}
	return syntax.DID(issuer), nil
}
