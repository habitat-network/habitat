package instanceadmin

import (
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/utils"

	"log/slog"
)

//go:embed login.html home.html style.css
var templateFS embed.FS

var templates = template.Must(
	template.ParseFS(templateFS, "login.html", "home.html", "style.css"),
)

// Server serves the instance admin login/logout flow, the admin settings
// page, and gates access to instance admin routes via session cookies.
type Server struct {
	store          Store
	frontendDomain string
}

func NewServer(store Store, frontendDomain string) *Server {
	return &Server{store: store, frontendDomain: frontendDomain}
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

// requireSessionPage reports whether the request carries a valid instance
// admin session, redirecting to the login page and returning false if not.
func (s *Server) requireSessionPage(w http.ResponseWriter, r *http.Request) bool {
	ok, err := s.store.ValidateSession(r)
	if err != nil {
		slog.ErrorContext(r.Context(), "validating instance admin session", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return false
	}
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return false
	}
	return true
}

// requireSessionAPI reports whether the request carries a valid instance
// admin session, writing a 401 and returning false if not.
func (s *Server) requireSessionAPI(w http.ResponseWriter, r *http.Request) bool {
	ok, err := s.store.ValidateSession(r)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"validating instance admin session",
			http.StatusInternalServerError,
		)
		return false
	}
	if !ok {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			errors.New("not authenticated"),
			"checking instance admin session",
			http.StatusUnauthorized,
		)
		return false
	}
	return true
}

func (s *Server) ServeAdminHome(w http.ResponseWriter, r *http.Request) {
	if !s.requireSessionPage(w, r) {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := struct{ FrontendDomain string }{s.frontendDomain}
	if err := templates.ExecuteTemplate(w, "home.html", data); err != nil {
		slog.ErrorContext(r.Context(), "rendering instance admin home page", "err", err)
	}
}

func (s *Server) GetSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireSessionAPI(w, r) {
		return
	}
	instanceName, policy, err := s.store.GetSettings(r.Context())
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"getting instance settings",
			http.StatusInternalServerError,
		)
		return
	}
	writeJSON(w, habitat.NetworkHabitatAdminGetSettingsOutput{
		InstanceName:      instanceName,
		OrgCreationPolicy: policy,
	})
}

func (s *Server) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireSessionAPI(w, r) {
		return
	}
	var req habitat.NetworkHabitatAdminUpdateSettingsInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "reading request body", http.StatusBadRequest)
		return
	}
	if err := s.store.UpdateSettings(
		r.Context(),
		req.InstanceName,
		req.OrgCreationPolicy,
	); err != nil {
		if errors.Is(err, ErrInvalidPolicy) {
			utils.LogAndHTTPError(
				r.Context(),
				w,
				err,
				"updating instance settings",
				http.StatusBadRequest,
			)
			return
		}
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"updating instance settings",
			http.StatusInternalServerError,
		)
		return
	}
	instanceName, orgCreationPolicy, err := s.store.GetSettings(r.Context())
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"getting instance settings",
			http.StatusInternalServerError,
		)
		return
	}
	writeJSON(w, habitat.NetworkHabitatAdminUpdateSettingsOutput{
		InstanceName:      instanceName,
		OrgCreationPolicy: orgCreationPolicy,
	})
}

func (s *Server) IssueInvite(w http.ResponseWriter, r *http.Request) {
	if !s.requireSessionAPI(w, r) {
		return
	}
	token, err := s.store.IssueInvite(r.Context())
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "issuing invite", http.StatusInternalServerError)
		return
	}
	writeJSON(w, habitat.NetworkHabitatAdminIssueInviteOutput{Token: token})
}

func (s *Server) DescribeInstance(w http.ResponseWriter, r *http.Request) {
	instanceName, policy, err := s.store.GetSettings(r.Context())
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"getting instance settings",
			http.StatusInternalServerError,
		)
		return
	}
	writeJSON(w, habitat.NetworkHabitatInstanceDescribeInstanceOutput{
		Name:           instanceName,
		InviteRequired: policy == policyInviteOnly,
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
