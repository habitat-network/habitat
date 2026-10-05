package authn

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/habitat-network/habitat/internal/httpx"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"

	"github.com/golang-jwt/jwt/v5"
)

// RevocationChecker reports whether a space credential was revoked by its
// space authority.
type RevocationChecker interface {
	IsRevoked(ctx context.Context, space habitat_syntax.SpaceURI, jti string) (bool, error)
}

type SpaceCredentialAuthMethod struct {
	dir         identity.Directory
	revocations RevocationChecker
}

var _ Method = (*SpaceCredentialAuthMethod)(nil)

func NewSpaceCredentialAuthMethod(
	directory identity.Directory,
	revocations RevocationChecker,
) *SpaceCredentialAuthMethod {
	return &SpaceCredentialAuthMethod{dir: directory, revocations: revocations}
}

// CanHandle implements [Method].
func (s *SpaceCredentialAuthMethod) CanHandle(r *http.Request) bool {
	token, err := getBearerJwt(r)
	if err != nil {
		return false
	}
	return token.Header["typ"] == "atproto-space-credential+jwt"
}

// Validate implements [Method].
func (s *SpaceCredentialAuthMethod) Validate(
	w http.ResponseWriter,
	r *http.Request,
	scopes ...string,
) (*CredentialInfo, bool) {
	ctx := r.Context()
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(
		getBearerToken(r),
		claims,
		fetchIssuerKeyFunc(ctx, s.dir, nil),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(time.Second*10),
	)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse token", err)
		return nil, false
	}
	space, err := getSpaceSubj(token.Claims)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "invalid space in token", err)
		return nil, false
	}
	issuer, _ /* issuer must exist from verification */ := token.Claims.GetIssuer()

	if issuer != string(space.SpaceOwner()) {
		httpx.WriteInvalidRequest(ctx, w, "token issuer does not match space", err)
		return nil, false
	}

	// Credentials minted before jti revocation existed may lack a jti; they
	// can't have been revoked, and still expire on their own.
	if jti, _ := claims["jti"].(string); jti != "" {
		revoked, err := s.revocations.IsRevoked(ctx, space, jti)
		if err != nil {
			httpx.WriteServerError(ctx, w, fmt.Errorf("check credential revocation: %w", err))
			return nil, false
		}
		if revoked {
			httpx.WriteUnauthorized(ctx, w, "credential revoked", nil)
			return nil, false
		}
	}

	return &CredentialInfo{
		Space:  space,
		Method: ValidatorMethodSpaceCredential,
	}, true
}
