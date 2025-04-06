package bffauth

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// GenerateJWT creates a signed JWT token containing the provided claims
// The claims are signed using HMAC with the provided signing key
func GenerateJWT(signingKey []byte, did string) (string, error) {
	claims := jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		Audience:  jwt.ClaimStrings([]string{did}),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, err := token.SignedString(signingKey)
	if err != nil {
		return "", fmt.Errorf("error signing token: %w", err)
	}

	return signedToken, nil
}

// ValidateJWT validates a JWT token and returns the claims
// The token is parsed, with the claims validated using the signing key
func ValidateJWT(tokenString string, signingKey []byte) (*jwt.RegisteredClaims, error) {
	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return signingKey, nil
	})

	if err != nil {
		return nil, fmt.Errorf("error parsing token: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	if claims.ExpiresAt != nil && claims.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("token has expired")
	}

	return claims, nil
}

func ValidateRequest(r *http.Request, signingKey []byte) error {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return fmt.Errorf("missing Authorization header")
	}

	const prefix = "Bearer "
	if !strings.HasPrefix(authHeader, prefix) {
		return fmt.Errorf("invalid Authorization header format")
	}

	token := authHeader[len(prefix):]
	if token == "" {
		return fmt.Errorf("missing token")
	}

	claims, err := ValidateJWT(token, signingKey)
	if err != nil {
		return fmt.Errorf("invalid token: %w", err)
	}

	if claims == nil {
		return fmt.Errorf("invalid claims")
	}

	return nil
}
