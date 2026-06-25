package oauthserver

import (
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
	// consentPath scopes the consent cookie to the consent page. It must stay
	// in sync between session creation and invalidation, since a Set-Cookie
	// only clears a cookie with a matching path.
	consentPath = "/oauth/consent"
	// consentUIPath is the embedded consent page UI (see internal/webui and
	// typescript/apps/pear-pages) that HandleConsent redirects the browser to.
	// HandleConsent passes the request-specific details (client ID, scopes,
	// org handle) as query params; the UI fetches the client's public metadata
	// document itself, and posts the admin's decision back to consentPath.
	consentUIPath = "/ui/oauth/consent"

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
		Path:     consentPath,
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

	http.Redirect(w, r, consentPath, http.StatusSeeOther)
}

// lookupConsent reads the consent cookie session off r without mutating it.
func (o *OAuthServer) lookupConsent(r *http.Request) (*consentFlash, error) {
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

	return &consentFlash{Form: form, Did: syntax.DID(did)}, nil
}

// invalidateConsentSession clears the consent cookie session so it can't be
// used again, e.g. once the admin's decision has been read off it.
func (o *OAuthServer) invalidateConsentSession(r *http.Request, w http.ResponseWriter) error {
	session, err := o.sessionStore.Get(r, consentSessionName)
	if err != nil {
		return err
	}
	clear(session.Values)
	// Get() loads Options from the store's defaults rather than whatever was
	// set in beginConsent (Options aren't part of the encoded cookie), so
	// Path must be set again here: a Set-Cookie only clears a cookie when its
	// path matches the one it was created with.
	session.Options.Path = consentPath
	session.Options.MaxAge = -1
	return session.Save(r, w)
}

// HandleConsent redirects the browser to the embedded consent page UI for a
// pending org DID authorization request, passing everything the UI renders
// (the requesting client's name/uri/logo, the requested scopes, and the org
// handle) as query params. The client's metadata is fetched server-side so
// the UI doesn't have to make a cross-origin request for the client's public
// metadata document.
func (o *OAuthServer) HandleConsent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cf, err := o.lookupConsent(r)
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

	fositeClient, err := o.storage.GetClient(ctx, cf.Form.Get("client_id"))
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

	id, err := o.directory.LookupDID(ctx, cf.Did)
	if err != nil {
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"failed to resolve org handle",
			http.StatusInternalServerError,
		)
		return
	}

	params := url.Values{}
	params.Set("clientName", c.ClientName)
	params.Set("clientUri", c.ClientUri)
	params.Set("logoUri", c.LogoUri)
	params.Set("scope", cf.Form.Get("scope"))
	params.Set("orgHandle", id.Handle.String())
	http.Redirect(w, r, consentUIPath+"?"+params.Encode(), http.StatusSeeOther)
}

// HandleConsentDecision processes the admin's accept/deny decision from the
// consent page. An authorization code is only issued on accept.
func (o *OAuthServer) HandleConsentDecision(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cf, err := o.lookupConsent(r)
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
	// Invalidate the session as soon as it's been read so it can't be
	// replayed even if the rest of this handler fails partway through.
	if err := o.invalidateConsentSession(r, w); err != nil {
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"failed to invalidate consent session",
			http.StatusInternalServerError,
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
