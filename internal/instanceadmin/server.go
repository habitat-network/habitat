package instanceadmin

import (
	"embed"
	"html/template"
	"net/http"

	"log/slog"
)

const sessionCookieName = "habitat_admin_session"
const sessionCookiePath = "/admin"

//go:embed login.html home.html style.css
var templateFS embed.FS

var templates = template.Must(
	template.ParseFS(templateFS, "login.html", "home.html", "style.css"),
)

// Server serves the instance admin login/logout flow and gates access to
// instance admin routes via session cookies.
type Server struct {
	store Store
}

func NewServer(store Store) *Server {
	return &Server{store: store}
}

func (s *Server) ServeLoginPage(w http.ResponseWriter, r *http.Request) {
	s.renderLogin(w, "")
}

func (s *Server) renderLogin(w http.ResponseWriter, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ExecuteTemplate(w, "login.html", struct{ Error string }{errMsg}); err != nil {
		slog.Error("rendering instance admin login page", "err", err)
	}
}

func (s *Server) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	password := r.FormValue("password")

	ok, err := s.store.Authenticate(r.Context(), password)
	if err != nil {
		slog.ErrorContext(r.Context(), "authenticating instance admin", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		s.renderLogin(w, "invalid password")
		return
	}

	token, expiresAt, err := s.store.CreateSession(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "creating instance admin session", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Path:     sessionCookiePath,
		Expires:  expiresAt,
	})
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Server) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if err := s.store.DeleteSession(r.Context(), cookie.Value); err != nil {
			slog.ErrorContext(r.Context(), "deleting instance admin session", "err", err)
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Path:     sessionCookiePath,
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// RequireSession wraps next so it is only called when the request carries a valid
// instance admin session cookie; otherwise it redirects to the login page.
func (s *Server) RequireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		ok, err := s.store.ValidateSession(r.Context(), cookie.Value)
		if err != nil {
			slog.ErrorContext(r.Context(), "validating instance admin session", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		next(w, r)
	}
}

func (s *Server) ServeAdminHome(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ExecuteTemplate(w, "home.html", nil); err != nil {
		slog.Error("rendering instance admin home page", "err", err)
	}
}
