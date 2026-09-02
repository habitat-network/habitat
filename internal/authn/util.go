package authn

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func getBearerJwt(r *http.Request) (token *jwt.Token, err error) {
	token, _, err = jwt.NewParser().
		ParseUnverified(getBearerToken(r), jwt.MapClaims{})
	return token, err
}

// getBearerToken extracts the token from an Authorization header, whether
// presented with the "Bearer" scheme or, per RFC 9449, the "DPoP" scheme
// used for DPoP-bound tokens such as space credentials.
func getBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	for _, prefix := range [...]string{"DPoP ", "Bearer "} {
		if trimmed := strings.TrimPrefix(auth, prefix); trimmed != auth {
			return trimmed
		}
	}
	return auth
}

func fetchIssuerKeyFunc(
	ctx context.Context,
	dir identity.Directory,
	hostKey atcrypto.PrivateKey,
) func(*jwt.Token) (any, error) {
	return func(token *jwt.Token) (any, error) {
		issuer, err := token.Claims.GetIssuer()
		if err != nil {
			return nil, fmt.Errorf("failed to get issuer: %w", err)
		}
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("failed to get kid: %s", issuer)
		}
		if kid == "#habitat" && hostKey != nil {
			publicKey, err := hostKey.PublicKey()
			if err != nil {
				return nil, fmt.Errorf("failed to get host public key: %w", err)
			}
			return publicKey, nil
		}
		issuerDID, err := syntax.ParseDID(issuer)
		if err != nil {
			return nil, fmt.Errorf("failed to parse issuer: %w", err)
		}
		ident, err := dir.LookupDID(ctx, issuerDID)
		if err != nil {
			return nil, fmt.Errorf("failed to lookup issuer: %w", err)
		}
		publicKey, err := ident.GetPublicKey(strings.TrimPrefix(kid, "#"))
		if err != nil {
			return nil, fmt.Errorf("failed to get public key: %w %s", err, kid)
		}
		return publicKey, nil
	}
}

func getSpaceSubj(claims jwt.Claims) (habitat_syntax.SpaceURI, error) {
	subj, err := claims.GetSubject()
	if err != nil {
		return "", fmt.Errorf("failed to get subject: %w", err)
	}
	space, err := habitat_syntax.ParseSpaceURI(subj)
	if err != nil {
		return "", fmt.Errorf("failed to parse space URI: %w", err)
	}
	return space, nil
}
