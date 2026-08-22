package notify_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/pearsetup/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
)

// spaceOwner is a resolvable identity (registered in a MockDirectory) that can
// sign its own space credentials. RegisterNotify only accepts the space
// credential auth method, which resolves the signer's key through the
// server's identity directory rather than through org membership — a
// space-owning DID does not need to be a hive-hosted org actor at all.
type spaceOwner struct {
	DID syntax.DID
	key atcrypto.PrivateKey
}

func newSpaceOwner(t *testing.T) (spaceOwner, *identity.MockDirectory) {
	t.Helper()
	key, err := atcrypto.GeneratePrivateKeyK256()
	require.NoError(t, err)
	pub, err := key.PublicKey()
	require.NoError(t, err)

	did := syntax.DID("did:plc:spaceowner")
	dir := identity.NewMockDirectory()
	dir.Insert(identity.Identity{
		DID: did,
		Keys: map[string]identity.VerificationMethod{
			"atproto": {Type: "Multikey", PublicKeyMultibase: pub.Multibase()},
		},
	})
	return spaceOwner{DID: did, key: key}, dir
}

// credential mints a real atproto-space-credential+jwt for space, signed by
// the owner's key.
func (o spaceOwner) credential(t *testing.T, space habitat_syntax.SpaceURI) string {
	t.Helper()
	token, err := utils.SpaceCredential(o.key, "#atproto", space)
	require.NoError(t, err)
	return "Bearer " + token
}

// registerNotifyRequest builds a registerNotify request authenticated as
// owner via a space credential, since Procedure only sends bearer actor
// tokens.
func registerNotifyRequest(
	t *testing.T,
	owner spaceOwner,
	space habitat_syntax.SpaceURI,
	repo, endpoint string,
) *http.Request {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"space":    space.String(),
		"repo":     repo,
		"endpoint": endpoint,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.registerNotify",
		bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", owner.credential(t, space))
	return req
}

func TestServerRegisterNotify(t *testing.T) {
	owner, dir := newSpaceOwner(t)
	p := testutil.New(t, testutil.WithDirectory(dir))
	space := habitat_syntax.ConstructSpaceURI(
		owner.DID, syntax.NSID("network.habitat.group"), habitat_syntax.SpaceKey("s1"),
	)

	resp := p.Do(nil, registerNotifyRequest(t, owner, space, "", "https://sync.example/all"))
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out habitat.NetworkHabitatSpaceRegisterNotifyOutput
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	expiresAt, err := time.Parse(time.RFC3339, out.ExpiresAt)
	require.NoError(t, err)
	require.True(t, expiresAt.After(time.Now()))

	regs, err := p.NotifyStore.ListForRepo(t.Context(), space, "")
	require.NoError(t, err)
	require.Len(t, regs, 1)
	require.Equal(t, "https://sync.example/all", regs[0].Endpoint)
	require.Empty(t, regs[0].Repo)
}

func TestServerRegisterNotifyRepoSpecific(t *testing.T) {
	owner, dir := newSpaceOwner(t)
	p := testutil.New(t, testutil.WithDirectory(dir))
	space := habitat_syntax.ConstructSpaceURI(
		owner.DID, syntax.NSID("network.habitat.group"), habitat_syntax.SpaceKey("s1"),
	)
	repo := syntax.DID("did:plc:alice")

	resp := p.Do(nil, registerNotifyRequest(t, owner, space, repo.String(), "https://sync.example/alice"))
	require.Equal(t, http.StatusOK, resp.StatusCode)

	regs, err := p.NotifyStore.ListForRepo(t.Context(), space, repo)
	require.NoError(t, err)
	require.Len(t, regs, 1)
	require.Equal(t, repo, regs[0].Repo)
}
