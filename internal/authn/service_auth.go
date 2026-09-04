package authn

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/auth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/org"
)

func NewServiceAuthMethod(
	everyoneOrg org.Org,
	directory identity.Directory,
	legacyDID syntax.DID,
	serviceEndpoint string,
) *AtprotoServiceAuthMethod {
	return &AtprotoServiceAuthMethod{
		everyoneOrg:     everyoneOrg,
		dir:             directory,
		serviceEndpoint: serviceEndpoint,
		legacyDID:       legacyDID,
	}
}

type AtprotoServiceAuthMethod struct {
	dir             identity.Directory
	serviceEndpoint string
	everyoneOrg     org.Org
	legacyDID       syntax.DID
}

var _ Validator = (*AtprotoServiceAuthMethod)(nil)

// CanHandle implements [Validator].
func (p *AtprotoServiceAuthMethod) CanHandle(r *http.Request) bool {
	tokenStr, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !found {
		return false
	}
	token, err := jwt.ParseSigned(tokenStr)
	if err != nil {
		return false
	}
	return token.Headers[0].ExtraHeaders["typ"] == "JWT"
}

// Validate implements [Validator].
func (p *AtprotoServiceAuthMethod) Validate(
	w http.ResponseWriter,
	r *http.Request,
	scopes ...string,
) (*CredentialInfo, bool) {
	ctx := r.Context()
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	_, nsidStr, _ := strings.Cut(r.URL.Path, "/xrpc/")
	nsid, err := syntax.ParseNSID(nsidStr)
	if err != nil {
		httpx.WriteUnauthorized(ctx, w, "failed to parse nsid")
		return nil, false
	}
	jwtToken, err := jwt.ParseSigned(token)
	if err != nil {
		httpx.WriteUnauthorized(ctx, w, "failed to parse token")
		return nil, false
	}
	claims := jwt.Claims{}
	if err := jwtToken.UnsafeClaimsWithoutVerification(&claims); err != nil {
		httpx.WriteUnauthorized(ctx, w, fmt.Sprintf("failed to parse token: %v", err))
		return nil, false
	}
	if len(claims.Audience) != 1 {
		httpx.WriteUnauthorized(ctx, w, "invalid aud claim")
		return nil, false
	}
	audienceDIDStr, audienceService, found := strings.Cut(claims.Audience[0], "#")
	audienceDID, err := syntax.ParseDID(audienceDIDStr)
	if err != nil {
		httpx.WriteUnauthorized(ctx, w, "invalid aud did")
		return nil, false
	}
	if !found {
		if audienceDID != p.legacyDID {
			httpx.WriteUnauthorized(ctx, w, "invalid aud claim")
			return nil, false
		}
	} else {
		audienceID, err := p.dir.LookupDID(ctx, audienceDID)
		if err != nil {
			httpx.WriteUnauthorized(ctx, w, "failed to lookup audience")
			return nil, false
		}
		if p.serviceEndpoint != audienceID.GetServiceEndpoint(audienceService) {
			httpx.WriteUnauthorized(ctx, w, "unexpected service endpoint")
			return nil, false
		}
	}

	// Audience is set to the token's own aud claim: we've already checked above
	// that it's either the legacy DID or resolves to our serviceEndpoint, so this
	// just satisfies the validator's own (non-optional) aud check.
	validator := &auth.ServiceAuthValidator{Dir: p.dir, Audience: claims.Audience[0]}
	did, err := validator.Validate(r.Context(), token, &nsid)
	if err != nil {
		httpx.WriteUnauthorized(ctx, w, "failed to validate token")
		return nil, false
	}
	return &CredentialInfo{
		Subject: did,
		Org:     p.everyoneOrg,
	}, true
}

func (p *AtprotoServiceAuthMethod) ValidateRaw(
	ctx context.Context,
	token string,
	scopes ...string,
) (*CredentialInfo, bool, error) {
	validator := &auth.ServiceAuthValidator{Dir: p.dir}
	did, err := validator.Validate(ctx, token, nil)
	if err != nil {
		return nil, false, err
	}
	return &CredentialInfo{Subject: did, Org: p.everyoneOrg}, true, nil
}
