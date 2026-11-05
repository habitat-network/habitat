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

	"github.com/habitat-network/habitat/internal/keyval"
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
	spent   keyval.Store
}

// delegationTokenLeeway is the clock skew a delegation token's iat and exp are
// checked with.
const delegationTokenLeeway = 10 * time.Second

// NewDelegationTokenVerifier verifies delegation tokens, recording each
// token's jti in spent so it can be presented only once.
func NewDelegationTokenVerifier(
	directory identity.Directory,
	srv SpaceRoleValidator,
	hostKey atcrypto.PrivateKey,
	spent keyval.Store,
) *DelegationTokenVerifier {
	return &DelegationTokenVerifier{
		dir:     directory,
		hostKey: hostKey,
		srv:     srv,
		spent:   spent,
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
		jwt.WithLeeway(delegationTokenLeeway),
		// The token is addressed to the space host: the authority's
		// #atproto_pds service.
		jwt.WithAudience(space.SpaceOwner().String()+"#atproto_pds"),
	)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidDelegationToken, err)
	}
	if typ := parsed.Header["typ"]; typ != "atproto-space-delegation+jwt" {
		return "", fmt.Errorf("%w: unexpected typ %v", ErrInvalidDelegationToken, typ)
	}
	// The proposal requires #atproto. #habitat is this host's own key, which
	// it signs with for users whose identity it doesn't manage.
	if kid := parsed.Header["kid"]; kid != "#atproto" && kid != "#habitat" {
		return "", fmt.Errorf("%w: unexpected kid %v", ErrInvalidDelegationToken, kid)
	}
	subj, err := getSpaceSubj(parsed.Claims)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidDelegationToken, err)
	}
	if subj != space {
		return "", fmt.Errorf("%w: token is for another space", ErrInvalidDelegationToken)
	}
	issuer, _ /* issuer must exist from verification */ := parsed.Claims.GetIssuer()
	if err := d.spend(ctx, issuer, parsed.Claims); err != nil {
		return "", err
	}
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

// spend records the token's jti so a replay of it is refused: a delegation
// token is single-use. The jti is remembered until the token expires.
func (d *DelegationTokenVerifier) spend(
	ctx context.Context,
	issuer string,
	claims jwt.Claims,
) error {
	mc, _ := claims.(jwt.MapClaims)
	jti, _ := mc["jti"].(string)
	if jti == "" {
		return fmt.Errorf("%w: missing jti", ErrInvalidDelegationToken)
	}
	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil {
		return fmt.Errorf("%w: missing exp", ErrInvalidDelegationToken)
	}
	ttl := time.Until(exp.Time) + delegationTokenLeeway
	err = d.spent.PutIfAbsent(ctx, issuer+" "+jti, true, ttl)
	if errors.Is(err, keyval.ErrAlreadyExists) {
		return fmt.Errorf("%w: token already used", ErrInvalidDelegationToken)
	} else if err != nil {
		return fmt.Errorf("record delegation token: %w", err)
	}
	return nil
}
