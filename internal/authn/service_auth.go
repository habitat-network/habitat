package authn

import (
	"context"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/auth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/utils"
)

func NewServiceAuthMethod(directory identity.Directory, audience string) *AtprotoServiceAuthMethod {
	return &AtprotoServiceAuthMethod{
		validator: &auth.ServiceAuthValidator{
			Dir:      directory,
			Audience: audience,
		},
	}
}

type AtprotoServiceAuthMethod struct {
	validator *auth.ServiceAuthValidator
}

var _ Method = (*AtprotoServiceAuthMethod)(nil)

// CanHandle implements [Method].
func (p *AtprotoServiceAuthMethod) CanHandle(r *http.Request) bool {
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
func (p *AtprotoServiceAuthMethod) Validate(
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

func (p *AtprotoServiceAuthMethod) ValidateRaw(
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
