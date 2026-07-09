package authn

import (
	"context"
	"fmt"
	"net/http"
	"strings"

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

func getBearerToken(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

func fetchIssuerKeyFunc(
	ctx context.Context,
	dir identity.Directory,
) func(*jwt.Token) (any, error) {
	return func(token *jwt.Token) (any, error) {
		issuer, err := token.Claims.GetIssuer()
		if err != nil {
			return nil, fmt.Errorf("failed to get issuer: %w", err)
		}
		issuerDID, err := syntax.ParseDID(issuer)
		if err != nil {
			return nil, fmt.Errorf("failed to parse issuer: %w", err)
		}
		ident, err := dir.LookupDID(ctx, issuerDID)
		if err != nil {
			return nil, fmt.Errorf("failed to lookup issuer: %w", err)
		}
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("failed to get kid")
		}
		publicKey, err := ident.GetPublicKey(kid)
		if err != nil {
			return nil, fmt.Errorf("failed to get public key: %w", err)
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
