package instance

import (
	"net/http"

	"github.com/gorilla/sessions"
)

// This file exposes instance's unexported identifiers to its external tests. It
// is compiled only into the test binary, so none of it is part of the package's
// API; the production identifiers stay unexported.

type (
	// StoreImpl is the concrete type NewStore returns.
	StoreImpl = storeImpl
)

const (
	// SessionName is the admin session cookie's name.
	SessionName = sessionName
	// SessionDuration is how long an admin session cookie lasts.
	SessionDuration = sessionDuration
)

// RequireSessionAPI calls the server's session guard directly, so the tests can
// exercise it without going through a handler.
func (s *Server) RequireSessionAPI(w http.ResponseWriter, r *http.Request) bool {
	return s.requireSessionAPI(w, r)
}

// SessionOptions returns the admin session cookie's options, which the tests
// assert are restrictive.
func (s *storeImpl) SessionOptions() *sessions.Options { return s.sessions.Options }

// The org-creation policies, which the settings tests set and assert on.
const (
	PolicyOpen       = policyOpen
	PolicyInviteOnly = policyInviteOnly
)
