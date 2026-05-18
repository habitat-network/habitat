package atprotoauth

// Server will be the atproto-spec OAuth authorization server. Today it only
// owns the issuer URL used by the discovery documents; PAR, /authorize,
// /token, and /jwks will land here as separate slices.
//
// It deliberately does not embed the legacy [oauthserver.OAuthServer]: the
// authorization flow shape is different enough that sharing a Server struct
// would tangle the two implementations. Where primitives are genuinely
// reusable (DPoP validation, JWT signing helpers, session storage) we'll
// extract them into shared packages rather than reach across.
type Server struct {
	issuer string
}

// NewServer constructs a Server. issuer is the canonical https URL of this
// authorization server (no trailing slash) and is used as the `iss` claim in
// issued tokens; mismatches between this and the URL the client discovered are
// a rejection signal in the atproto OAuth spec.
func NewServer(issuer string) *Server {
	return &Server{issuer: issuer}
}

// Issuer returns the canonical issuer URL.
func (s *Server) Issuer() string { return s.issuer }
