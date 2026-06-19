package instanceadmin

import (
	"embed"
	"html/template"
	"net/http"

	"log/slog"
)

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
	s.renderLogin(r, w, "")
}

func (s *Server) renderLogin(r *http.Request, w http.ResponseWriter, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ExecuteTemplate(
		w,
		"login.html",
		struct{ Error string }{errMsg},
	); err != nil {
		slog.ErrorContext(r.Context(), "rendering instance admin login page", "err", err)
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
		s.renderLogin(r, w, "invalid password")
		return
	}

	if err := s.store.CreateSession(w, r); err != nil {
		slog.ErrorContext(r.Context(), "creating instance admin session", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Server) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteSession(w, r); err != nil {
		slog.ErrorContext(r.Context(), "deleting instance admin session", "err", err)
	}
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// RequireSession wraps next so it is only called when the request carries a valid
// instance admin session cookie; otherwise it redirects to the login page.
func (s *Server) RequireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ok, err := s.store.ValidateSession(r)
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
		slog.ErrorContext(r.Context(), "rendering instance admin home page", "err", err)
	}
}
