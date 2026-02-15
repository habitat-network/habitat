package authmethods

import (
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/go-jose/go-jose/v3"
	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/habitat-network/habitat/internal/utils"
)

func NewServiceAuthMethod(directory identity.Directory) Method {
	return &pdsServiceAuthMethod{
		directory: directory,
	}
}

type pdsServiceAuthMethod struct {
	directory identity.Directory
}

var _ Method = (*pdsServiceAuthMethod)(nil)

// CanHandle implements [Method].
func (p *pdsServiceAuthMethod) CanHandle(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
}

// Validate implements [Method].
func (p *pdsServiceAuthMethod) Validate(
	w http.ResponseWriter,
	r *http.Request,
	scopes ...string,
) (syntax.DID, bool) {
	token, err := jwt.ParseSigned(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if err != nil {
		utils.WriteHTTPError(w, err, http.StatusUnauthorized)
		return "", false
	}
	var claims serviceJwtPayload
	if err := token.UnsafeClaimsWithoutVerification(&claims); err != nil {
		utils.WriteHTTPError(w, err, http.StatusUnauthorized)
		return "", false
	}
	issuerDid, err := syntax.ParseDID(claims.Iss)
	if err != nil {
		utils.WriteHTTPError(w, err, http.StatusUnauthorized)
		return "", false
	}
	id, err := p.directory.LookupDID(r.Context(), issuerDid)
	if err != nil {
		utils.WriteHTTPError(w, err, http.StatusUnauthorized)
		return "", false
	}
	publicKey, err := id.PublicKey()
	if err != nil {
		utils.WriteHTTPError(w, err, http.StatusUnauthorized)
		return "", false
	}
	if err := token.Claims(atcryptoVerifier{publicKey}); err != nil {
		utils.WriteHTTPError(w, err, http.StatusUnauthorized)
		return "", false
	}
	// TODO: validate Aud and Lxm
	return issuerDid, true
}

type serviceJwtPayload struct {
	Iss string `json:"iss"`
	Aud string `json:"aud"`
	Exp int64  `json:"exp"`
	Lxm string `json:"lxm"`
}

type atcryptoVerifier struct {
	atcrypto.PublicKey
}

var _ jose.OpaqueVerifier = (*atcryptoVerifier)(nil)

// VerifyPayload implements [jose.OpaqueVerifier].
func (a atcryptoVerifier) VerifyPayload(
	payload []byte,
	signature []byte,
	alg jose.SignatureAlgorithm,
) error {
	return a.HashAndVerify(payload, signature)
}
