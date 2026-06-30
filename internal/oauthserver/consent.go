package oauthserver

import (
	"encoding/gob"
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
	// typescript/apps/pear-pages) that beginConsent redirects the browser to,
	// passing the request details (client metadata, scopes, org handle) as
	// query params. The UI posts the admin's decision back to consentPath.
	consentUIPath = "/ui/oauth/consent"

	// consentMaxAge bounds how long the consent page may sit unanswered
	// before its cookie expires.
	consentMaxAge = 10 * time.Minute
)

// pendingConsent holds the authorization request carried, as a one-shot flash,
// in the signed cookie session set while an org admin reviews the client's
// consent screen.
type pendingConsent struct {
	Form url.Values
	Did  syntax.DID
}

func init() {
	// Flash values are gob-encoded into the session cookie, so the concrete
	// type stored via AddFlash must be registered.
	gob.Register(pendingConsent{})
}

// beginConsent fetches the details the consent page renders, persists the
// pending authorization request in a signed cookie session for
// HandleConsentDecision to act on, and redirects the browser straight to the
// consent page UI with those details as query params. The client metadata is
// fetched server-side, so the UI needs neither the session nor a cross-origin
// request to render.
func (o *OAuthServer) beginConsent(
	w http.ResponseWriter,
	r *http.Request,
	form url.Values,
	did syntax.DID,
) {
	ctx := r.Context()

	fositeClient, err := o.storage.GetClient(ctx, form.Get("client_id"))
	if err != nil {
		o.metrics.callbackErr(ctx, err, "fetch_client")
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

	id, err := o.directory.LookupDID(ctx, did)
	if err != nil {
		o.metrics.callbackErr(ctx, err, "lookup_org_handle")
		utils.LogAndHTTPError(
			ctx,
			w,
			err,
			"failed to resolve org handle",
			http.StatusInternalServerError,
		)
		return
	}

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
	session.AddFlash(pendingConsent{Form: form, Did: did})
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

	params := url.Values{}
	params.Set("clientId", form.Get("client_id"))
	params.Set("clientName", c.ClientName)
	params.Set("clientUri", c.ClientUri)
	params.Set("logoUri", c.LogoUri)
	params.Set("scope", form.Get("scope"))
	params.Set("orgHandle", id.Handle.String())
	http.Redirect(w, r, consentUIPath+"?"+params.Encode(), http.StatusSeeOther)
}

// popConsent reads the one-shot consent flash off r and deletes the cookie, so
// the pending request can't be replayed once the admin's decision is acted on.
func (o *OAuthServer) popConsent(r *http.Request, w http.ResponseWriter) (*pendingConsent, error) {
	session, err := o.sessionStore.Get(r, consentSessionName)
	if err != nil {
		return nil, err
	}

	flashes := session.Flashes()
	if len(flashes) == 0 {
		return nil, http.ErrNoCookie
	}
	pc, ok := flashes[0].(pendingConsent)
	if !ok {
		return nil, http.ErrNoCookie
	}

	// Flashes() drops the value from the session; deleting the cookie too keeps
	// a spent consent cookie from lingering in the browser. Get() loads Options
	// from the store's defaults rather than whatever beginConsent set (Options
	// aren't part of the encoded cookie), so Path must be set again here: a
	// Set-Cookie only clears a cookie when its path matches the original.
	session.Options.Path = consentPath
	session.Options.MaxAge = -1
	if err := session.Save(r, w); err != nil {
		return nil, err
	}

	return &pc, nil
}

// HandleConsentDecision processes the admin's accept/deny decision from the
// consent page. An authorization code is only issued on accept.
func (o *OAuthServer) HandleConsentDecision(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cf, err := o.popConsent(r, w)
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
