package oauthserver

import (
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/gorilla/sessions"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/ory/fosite"
)

const (
	consentSessionName = "consent-session"

	consentFormKey = "form"
	consentDidKey  = "did"

	// consentMaxAge bounds how long the consent page may sit unanswered
	// before its flash cookie expires.
	consentMaxAge = 10 * time.Minute
)

// consentFlash holds the pending authorization request decoded from the
// signed cookie session set while an org admin reviews the client's consent
// screen.
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

// beginConsent stores the pending authorization request in a signed cookie
// session and redirects the browser to the consent page.
func (o *OAuthServer) beginConsent(
	w http.ResponseWriter,
	r *http.Request,
	form url.Values,
	did syntax.DID,
) {
	ctx := r.Context()
	session, err := o.sessionStore.New(r, consentSessionName)
	if err != nil {
		o.metrics.callbackErr(ctx, err, "new_consent_session")
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"failed to create consent session",
			http.StatusInternalServerError,
		)
		return
	}
	session.Options = &sessions.Options{
		Path:     "/oauth/consent",
		MaxAge:   int(consentMaxAge.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
	session.Values[consentFormKey] = form.Encode()
	session.Values[consentDidKey] = string(did)
	if err := session.Save(r, w); err != nil {
		o.metrics.callbackErr(ctx, err, "save_consent_session")
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"failed to save consent session",
			http.StatusInternalServerError,
		)
		return
	}

	http.Redirect(w, r, "/oauth/consent", http.StatusSeeOther)
}

// lookupConsent reads the consent cookie session off r. If pop is true, the
// session is invalidated so it can only be used once.
func (o *OAuthServer) lookupConsent(
	r *http.Request,
	w http.ResponseWriter,
	pop bool,
) (*consentFlash, error) {
	session, err := o.sessionStore.Get(r, consentSessionName)
	if err != nil {
		return nil, err
	}

	formStr, ok := session.Values[consentFormKey].(string)
	if !ok {
		return nil, http.ErrNoCookie
	}
	form, err := url.ParseQuery(formStr)
	if err != nil {
		return nil, err
	}
	did, _ := session.Values[consentDidKey].(string)

	if pop {
		clear(session.Values)
		session.Options.MaxAge = -1
		if err := session.Save(r, w); err != nil {
			return nil, err
		}
	}

	return &consentFlash{Form: form, Did: syntax.DID(did)}, nil
}

// HandleConsent renders the consent page for a pending org DID authorization
// request, showing the requesting client's metadata and requested scopes.
func (o *OAuthServer) HandleConsent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cf, err := o.lookupConsent(r, w, false)
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
	cf, err := o.lookupConsent(r, w, true)
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
