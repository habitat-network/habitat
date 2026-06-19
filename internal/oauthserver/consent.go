package oauthserver

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/ory/fosite"
)

const consentSessionName = "consent-session"

// consentFlash stores the pending authorization request while an org admin
// reviews the client's consent screen.
type consentFlash struct {
	Form url.Values
	Did  syntax.DID
}

//go:embed consent.html
var consentTemplateFS embed.FS

var consentTemplates = template.Must(template.ParseFS(consentTemplateFS, "consent.html"))

// consentPageData is rendered into consent.html.
type consentPageData struct {
	ClientName string
	ClientUri  string
	LogoUri    string
	Scopes     []string
	OrgDID     string
}

// beginConsent stores the pending authorization request behind a new opaque
// cookie and redirects the browser to the consent page.
func (o *OAuthServer) beginConsent(
	w http.ResponseWriter,
	r *http.Request,
	form url.Values,
	did syntax.DID,
) {
	ctx := r.Context()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		o.metrics.callbackErr(ctx, err, "gen_consent_id")
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"failed to generate consent session id",
			http.StatusInternalServerError,
		)
		return
	}
	consentID := hex.EncodeToString(b)

	o.consentMu.Lock()
	o.consentStore[consentID] = &consentFlash{Form: form, Did: did}
	o.consentMu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     consentSessionName,
		Value:    consentID,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/oauth/consent",
	})

	http.Redirect(w, r, "/oauth/consent", http.StatusSeeOther)
}

// lookupConsent reads the consent cookie off r and returns the associated
// flash. If pop is true, the flash is removed so it can only be used once.
func (o *OAuthServer) lookupConsent(r *http.Request, pop bool) (*consentFlash, error) {
	cookie, err := r.Cookie(consentSessionName)
	if err != nil {
		return nil, err
	}

	o.consentMu.Lock()
	cf, ok := o.consentStore[cookie.Value]
	if pop {
		delete(o.consentStore, cookie.Value)
	}
	o.consentMu.Unlock()
	if !ok {
		return nil, http.ErrNoCookie
	}
	return cf, nil
}

// HandleConsent renders the consent page for a pending org DID authorization
// request, showing the requesting client's metadata and requested scopes.
func (o *OAuthServer) HandleConsent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cf, err := o.lookupConsent(r, false)
	if err != nil {
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"no consent state found for session",
			http.StatusBadRequest,
		)
		return
	}

	authRequest, err := o.recreateAuthorizeRequest(ctx, cf.Form)
	if err != nil {
		utils.LogAndHTTPError(ctx, w, err, "failed to recreate request", http.StatusBadRequest)
		return
	}

	fositeClient, err := o.storage.GetClient(ctx, authRequest.GetClient().GetID())
	if err != nil {
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"failed to fetch client metadata",
			http.StatusInternalServerError,
		)
		return
	}
	c := fositeClient.(*client)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = consentTemplates.ExecuteTemplate(w, "consent.html", consentPageData{
		ClientName: c.ClientName,
		ClientUri:  c.ClientUri,
		LogoUri:    c.LogoUri,
		Scopes:     authRequest.GetRequestedScopes(),
		OrgDID:     cf.Did.String(),
	})
	if err != nil {
		slog.ErrorContext(ctx, "rendering consent page", "err", err)
	}
}

// HandleConsentDecision processes the admin's accept/deny decision from the
// consent page. An authorization code is only issued on accept.
func (o *OAuthServer) HandleConsentDecision(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cf, err := o.lookupConsent(r, true)
	if err != nil {
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"no consent state found for session",
			http.StatusBadRequest,
		)
		return
	}

	if err := r.ParseForm(); err != nil {
		utils.LogAndHTTPError(ctx, w, err, "failed to parse form", http.StatusBadRequest)
		return
	}

	authRequest, err := o.recreateAuthorizeRequest(ctx, cf.Form)
	if err != nil {
		utils.LogAndHTTPError(ctx, w, err, "failed to recreate request", http.StatusBadRequest)
		return
	}

	if r.PostForm.Get("decision") != "allow" {
		o.provider.WriteAuthorizeError(
			ctx,
			w,
			authRequest,
			fosite.ErrAccessDenied.WithHint("The resource owner denied the consent request."),
		)
		return
	}

	resp, err := o.provider.NewAuthorizeResponse(
		ctx,
		authRequest,
		newAuthorizeSession(authRequest, cf.Did),
	)
	if err != nil {
		o.metrics.callbackErr(ctx, err, fositeErrReason(err))
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"failed to create response",
			http.StatusInternalServerError,
		)
		return
	}
	o.provider.WriteAuthorizeResponse(ctx, w, authRequest, resp)
	o.metrics.callbackSuccess()
}
