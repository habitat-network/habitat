package org

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	jose "github.com/go-jose/go-jose/v3"
	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/habitat-network/habitat/api/habitat"
)

var errInvalidLoginToken = errors.New("invalid or expired login token")

type loginTokenClaims struct {
	jwt.Claims
}

// LoginProvider wraps Org and implements login.Provider for habitat-hosted member identities.
type LoginProvider struct {
	org            Org
	frontendDomain string
	signingSecret  []byte
}

func NewLoginProvider(o Org, frontendDomain string, signingSecret []byte) *LoginProvider {
	return &LoginProvider{
		org:            o,
		frontendDomain: frontendDomain,
		signingSecret:  signingSecret,
	}
}

func (p *LoginProvider) Type() string { return "habitat" }

func (p *LoginProvider) CanHandle(id *identity.Identity) bool {
	_, hasHabitat := id.Services["habitat"]
	_, hasPDS := id.Services["atproto_pds"]
	return hasHabitat && !hasPDS
}

func (p *LoginProvider) Authorize(_ context.Context, id *identity.Identity) (string, []byte, error) {
	redirect := "https://" + p.frontendDomain + "/login/habitat?handle=" + url.QueryEscape(string(id.Handle))
	return redirect, nil, nil
}

func (p *LoginProvider) issueToken() (string, error) {
	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: p.signingSecret}, nil)
	if err != nil {
		return "", err
	}
	claims := loginTokenClaims{
		Claims: jwt.Claims{
			Expiry: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
		},
	}
	return jwt.Signed(sig).Claims(claims).CompactSerialize()
}

func (p *LoginProvider) verifyToken(token string) error {
	parsed, err := jwt.ParseSigned(token)
	if err != nil {
		return errInvalidLoginToken
	}
	var claims loginTokenClaims
	if err := parsed.Claims(p.signingSecret, &claims); err != nil {
		return errInvalidLoginToken
	}
	if err := claims.ValidateWithLeeway(jwt.Expected{Time: time.Now()}, 0); err != nil {
		return errInvalidLoginToken
	}
	return nil
}

func (p *LoginProvider) Exchange(_ context.Context, _ syntax.DID, code, _ string, _ []byte) error {
	return p.verifyToken(code)
}

func (p *LoginProvider) HandlePasswordLogin(w http.ResponseWriter, r *http.Request) {
	var req habitat.NetworkHabitatOrgLoginMemberInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	ok, err := p.org.AuthenticateMember(r.Context(), req.Handle, req.Password)
	if err != nil {
		http.Error(w, "error while authenticating", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	token, err := p.issueToken()
	if err != nil {
		http.Error(w, "error while issuing callback token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(habitat.NetworkHabitatOrgLoginMemberOutput{
		CallbackURL: "/oauth-callback?code=" + token,
	})
	if err != nil {
		http.Error(w, "encoding response", http.StatusInternalServerError)
		return
	}
}
