package org

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	jose "github.com/go-jose/go-jose/v3"
	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/utils"
)

var errInvalidLoginToken = errors.New("invalid or expired login token")

type loginTokenClaims struct {
	jwt.Claims
}

// LoginProvider wraps Store and implements login.Provider for habitat-hosted member identities.
type LoginProvider struct {
	store          Store
	pearDomain     string
	frontendDomain string
	signingSecret  []byte
	dir            identity.Directory
}

func NewLoginProvider(
	store Store,
	pearDomain string,
	frontendDomain string,
	signingSecret []byte,
	dir identity.Directory,
) *LoginProvider {
	return &LoginProvider{
		store:          store,
		pearDomain:     pearDomain,
		frontendDomain: frontendDomain,
		signingSecret:  signingSecret,
		dir:            dir,
	}
}

func (p *LoginProvider) LoginMethod() LoginMethod { return LoginMethodPassword }

func (p *LoginProvider) Authorize(
	_ context.Context,
	id *identity.Identity,
	_ string,
) (redirect string, state []byte, err error) {
	redirect = "https://" + p.frontendDomain + "/login/habitat?handle=" + url.QueryEscape(
		string(id.Handle),
	)
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
		return fmt.Errorf("%w: %w", errInvalidLoginToken, err)
	}
	var claims loginTokenClaims
	if err := parsed.Claims(p.signingSecret, &claims); err != nil {
		return fmt.Errorf("%w: %w", errInvalidLoginToken, err)
	}
	if err := claims.ValidateWithLeeway(jwt.Expected{Time: time.Now()}, 0); err != nil {
		return fmt.Errorf("%w: %w", errInvalidLoginToken, err)
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

	atid, err := syntax.ParseAtIdentifier(req.Handle)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parsing at identifier", http.StatusBadRequest)
		return
	}

	id, err := p.dir.Lookup(r.Context(), atid)
	if err != nil {
		http.Error(w, "invalid handle", http.StatusUnauthorized)
		return
	}

	member, err := p.store.GetMember(r.Context(), id.DID)
	if err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	ok, err := verifyPassword(req.Password, member.LoginID)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"error while authenticating",
			http.StatusInternalServerError,
		)
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
		CallbackURL: "https://" + p.pearDomain + "/oauth-callback?code=" + token,
	})
	if err != nil {
		http.Error(w, "encoding response", http.StatusInternalServerError)
		return
	}
}
