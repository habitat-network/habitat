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
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/utils"
)

// IsValidAudience reports whether aud (a "<did>#<serviceId>" string, as read
// from an incoming service-auth token's own "aud" claim) names a service
// this instance may legitimately validate tokens for. A single-identity
// service can just compare against one fixed value; an instance hosting
// many DIDs (pear, via hive) checks whether it actually hosts the named DID.
type IsValidAudience func(ctx context.Context, aud string) bool

// FixedAudience returns an IsValidAudience that accepts exactly one
// audience string, for a service with a single, unchanging identity.
func FixedAudience(audience string) IsValidAudience {
	return func(ctx context.Context, aud string) bool {
		return aud == audience
	}
}

func NewServiceAuthMethod(
	everyoneOrg org.Org,
	directory identity.Directory,
	isValidAudience IsValidAudience,
) *AtprotoServiceAuthMethod {
	return &AtprotoServiceAuthMethod{
		everyoneOrg:     everyoneOrg,
		dir:             directory,
		isValidAudience: isValidAudience,
	}
}

type AtprotoServiceAuthMethod struct {
	dir             identity.Directory
	isValidAudience IsValidAudience
	everyoneOrg     org.Org
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
	// "kid" isn't required: Validate resolves the signing key from "iss" via
	// directory lookup, never from "kid" (see atproto/auth.ServiceAuthValidator),
	// and a real PDS's own com.atproto.server.getServiceAuth response (used by
	// internal/forwarding for a non-hive-hosted identity) may omit it entirely.
	// "typ" alone already disambiguates from an OAuth JWT, whose typ is
	// "oauth+JWT", not "JWT" (see OAuthServer.CanHandle).
	return token.Headers[0].ExtraHeaders["typ"] == "JWT"
}

// Validate implements [Validator].
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
	did, err := p.validate(r.Context(), token, &nsid)
	if err != nil {
		utils.WriteHTTPError(w, err, http.StatusBadRequest)
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
	did, err := p.validate(ctx, token, nil)
	if err != nil {
		return nil, false, err
	}
	return &CredentialInfo{Subject: did, Org: p.everyoneOrg}, true, nil
}

// validate reads the token's own "aud" claim to learn which of this
// instance's hosted services the caller means to act into (pear hosts many
// DIDs — every org, every hive-served member — not one fixed identity), and
// only proceeds once isValidAudience independently confirms that audience is
// actually one this instance may act as. Signature and claims verification
// (including a redundant, now-safe check that the token's aud matches what
// we just confirmed) still happens via atproto/auth.ServiceAuthValidator.
func (p *AtprotoServiceAuthMethod) validate(
	ctx context.Context,
	tokenStr string,
	nsid *syntax.NSID,
) (syntax.DID, error) {
	unverified, err := jwt.ParseSigned(tokenStr)
	if err != nil {
		return "", fmt.Errorf("parse token: %w", err)
	}
	var claims struct {
		Audience string `json:"aud"`
	}
	if err := unverified.UnsafeClaimsWithoutVerification(&claims); err != nil {
		return "", fmt.Errorf("parse claims: %w", err)
	}
	if !p.isValidAudience(ctx, claims.Audience) {
		return "", fmt.Errorf("audience %q is not valid for this instance", claims.Audience)
	}
	validator := &auth.ServiceAuthValidator{Dir: p.dir, Audience: claims.Audience}
	return validator.Validate(ctx, tokenStr, nsid)
}
