package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/org"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// spaceCredMethod is a test authn.Method that authenticates every request as a
// credential for the given space.
type spaceCredMethod struct {
	space habitat_syntax.SpaceURI
}

func (m *spaceCredMethod) CanHandle(_ *http.Request) bool { return true }
func (m *spaceCredMethod) Validate(
	_ http.ResponseWriter,
	_ *http.Request,
	_ ...string,
) (*authn.CredentialInfo, bool) {
	return &authn.CredentialInfo{Space: m.space}, true
}

// oauthMethod is a test authn.Method that authenticates every request as the
// given member DID (no space credential), like an OAuth token.
type oauthMethod struct {
	did syntax.DID
}

func (m *oauthMethod) CanHandle(_ *http.Request) bool { return true }
func (m *oauthMethod) Validate(
	_ http.ResponseWriter,
	_ *http.Request,
	_ ...string,
) (*authn.CredentialInfo, bool) {
	return &authn.CredentialInfo{Subject: m.did, Org: &org.EveryoneOrg{}}, true
}

// fakeMembers is a MembershipChecker returning a fixed answer.
type fakeMembers struct {
	member bool
}

func (f *fakeMembers) IsMember(
	_ context.Context,
	_ syntax.DID,
	_ habitat_syntax.SpaceURI,
	_ syntax.DID,
) (bool, error) {
	return f.member, nil
}

func newTestServer(t *testing.T, credSpace habitat_syntax.SpaceURI) *Server {
	t.Helper()
	return NewServer(newTestStore(t), &fakeMembers{}, &spaceCredMethod{space: credSpace})
}

func registerNotifyReq(body string) *http.Request {
	return httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.registerNotify",
		strings.NewReader(body),
	)
}

func TestServerRegisterNotify(t *testing.T) {
	s := newTestServer(t, space)

	body := `{"space": "` + space.String() + `", "endpoint": "https://sync.example/all"}`
	w := httptest.NewRecorder()
	s.RegisterNotify(w, registerNotifyReq(body))

	require.Equal(t, http.StatusOK, w.Code)
	var out habitat.NetworkHabitatSpaceRegisterNotifyOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	expiresAt, err := time.Parse(time.RFC3339, out.ExpiresAt)
	require.NoError(t, err)
	require.True(t, expiresAt.After(time.Now()))

	regs, err := s.store.ListForRepo(t.Context(), space, repo)
	require.NoError(t, err)
	require.Len(t, regs, 1)
	require.Equal(t, "https://sync.example/all", regs[0].Endpoint)
	require.Empty(t, regs[0].Repo)
}

func TestServerRegisterNotifyRepoSpecific(t *testing.T) {
	s := newTestServer(t, space)

	body := `{"space": "` + space.String() + `", "repo": "` + repo.String() +
		`", "endpoint": "https://sync.example/alice"}`
	w := httptest.NewRecorder()
	s.RegisterNotify(w, registerNotifyReq(body))

	require.Equal(t, http.StatusOK, w.Code)
	regs, err := s.store.ListForRepo(t.Context(), space, repo)
	require.NoError(t, err)
	require.Len(t, regs, 1)
	require.Equal(t, repo, regs[0].Repo)
}

func TestServerRegisterNotifyOAuthMember(t *testing.T) {
	// A member's OAuth token (no space credential) registers when the membership
	// check passes.
	s := NewServer(newTestStore(t), &fakeMembers{member: true},
		&oauthMethod{did: "did:plc:member"})

	body := `{"space": "` + space.String() + `", "endpoint": "https://sync.example/all"}`
	w := httptest.NewRecorder()
	s.RegisterNotify(w, registerNotifyReq(body))

	require.Equal(t, http.StatusOK, w.Code)
	regs, err := s.store.ListForSpace(t.Context(), space)
	require.NoError(t, err)
	require.Len(t, regs, 1)
}

func TestServerRegisterNotifyOAuthNonMember(t *testing.T) {
	// A non-member's OAuth token is rejected and nothing is registered.
	s := NewServer(newTestStore(t), &fakeMembers{member: false},
		&oauthMethod{did: "did:plc:stranger"})

	body := `{"space": "` + space.String() + `", "endpoint": "https://sync.example/all"}`
	w := httptest.NewRecorder()
	s.RegisterNotify(w, registerNotifyReq(body))

	require.Equal(t, http.StatusBadRequest, w.Code)
	regs, err := s.store.ListForSpace(t.Context(), space)
	require.NoError(t, err)
	require.Empty(t, regs)
}

func TestServerRegisterNotifyRejectsSpaceMismatch(t *testing.T) {
	// The credential authorizes a different space than the one in the body.
	other := habitat_syntax.SpaceURI("ats://did:plc:org/network.habitat.group/other")
	s := newTestServer(t, other)

	body := `{"space": "` + space.String() + `", "endpoint": "https://sync.example/all"}`
	w := httptest.NewRecorder()
	s.RegisterNotify(w, registerNotifyReq(body))

	require.Equal(t, http.StatusBadRequest, w.Code)
	regs, err := s.store.ListForRepo(t.Context(), space, repo)
	require.NoError(t, err)
	require.Empty(t, regs)
}
