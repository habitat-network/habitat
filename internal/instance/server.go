package instance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/utils"

	"log/slog"
)

// Paths of the instance admin pages, served as pre-rendered static pages by the
// pear-pages app embedded under /ui/ (see internal/webui).
const (
	adminLoginPath = "/ui/admin/login"
	adminHomePath  = "/ui/admin"
)

// Server serves the instance admin login/logout flow, the admin settings
// page, and gates access to instance admin routes via session cookies.
type Server struct {
	store          AdminStore
	frontendDomain string
}

func NewServer(store AdminStore, frontendDomain string) *Server {
	return &Server{store: store, frontendDomain: frontendDomain}
}

// ServeLoginPage redirects to the embedded admin login page under /ui/.
func (s *Server) ServeLoginPage(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, adminLoginPath, http.StatusSeeOther)
}

// ServeConfig returns instance configuration the admin page needs at runtime
// (e.g. the frontend domain used to build invite links). Gated by the admin
// session.
func (s *Server) ServeConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireSessionAPI(w, r) {
		return
	}
	writeJSON(w, struct {
		FrontendDomain string `json:"frontendDomain"`
	}{s.frontendDomain})
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
		http.Redirect(
			w,
			r,
			adminLoginPath+"?error="+url.QueryEscape("invalid password"),
			http.StatusSeeOther,
		)
		return
	}

	if err := s.store.CreateSession(w, r); err != nil {
		slog.ErrorContext(r.Context(), "creating instance admin session", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, adminHomePath, http.StatusSeeOther)
}

func (s *Server) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteSession(w, r); err != nil {
		slog.ErrorContext(r.Context(), "deleting instance admin session", "err", err)
	}
	http.Redirect(w, r, adminLoginPath, http.StatusSeeOther)
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

// ServeAdminHome redirects to the embedded admin settings page under /ui/. The
// page itself fetches settings via the session-gated admin APIs and redirects
// to the login page if the session is missing.
func (s *Server) ServeAdminHome(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, adminHomePath, http.StatusSeeOther)
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
		OrgCreationPolicy: string(policy),
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
		InvitePolicy(req.OrgCreationPolicy),
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
		OrgCreationPolicy: string(orgCreationPolicy),
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
	httpx.WriteJSON(context.Background(), w, v)
}
