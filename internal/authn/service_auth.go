package authn

import (
	"context"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/go-jose/go-jose/v3"
	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/utils"
)

func NewServiceAuthMethod(directory identity.Directory, audience string) Method {
	return &pdsServiceAuthMethod{
		validator: &auth.ServiceAuthValidator{
			Dir:      directory,
			Audience: audience,
		},
	}
}

type pdsServiceAuthMethod struct {
	validator *auth.ServiceAuthValidator
}

var _ Method = (*pdsServiceAuthMethod)(nil)

// CanHandle implements [Method].
func (p *pdsServiceAuthMethod) CanHandle(r *http.Request) bool {
	tokenStr, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !found {
		return false
	}
	token, err := jwt.ParseSigned(tokenStr)
	if err != nil {
		return false
	}
	return token.Headers[0].ExtraHeaders["typ"] == "JWT" &&
		strings.HasPrefix(token.Headers[0].KeyID, "#")
}

// Validate implements [Method].
func (p *pdsServiceAuthMethod) Validate(
	w http.ResponseWriter,
	r *http.Request,
	scopes ...string,
) (*CredentialInfo, bool) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	_, nsidStr, _ := strings.Cut(r.URL.Path, "/xrpc/")
	nsid, err := syntax.ParseNSID(nsidStr)
	if err != nil {
		utils.WriteHTTPError(w, err, http.StatusBadRequest)
		return nil, false
	}
	did, err := p.validator.Validate(r.Context(), token, &nsid)
	if err != nil {
		utils.WriteHTTPError(w, err, http.StatusBadRequest)
		return nil, false
	}
	return &CredentialInfo{
		Subject: did,
		Type:    UserCredential,
		Org:     &org.EveryoneOrg{},
	}, true
}

func (p *pdsServiceAuthMethod) ValidateRaw(
	ctx context.Context,
	token string,
	scopes ...string,
) (*CredentialInfo, bool, error) {
	did, err := p.validator.Validate(ctx, token, nil)
	if err != nil {
		return nil, false, err
	}
	return &CredentialInfo{Subject: did, Type: UserCredential, Org: &org.EveryoneOrg{}}, true, nil
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
