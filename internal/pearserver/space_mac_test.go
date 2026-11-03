package pearserver_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	"github.com/habitat-network/habitat/internal/spacecommit"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// macCall sends a GET to handler with params and, if wantMAC is set, an
// Atproto-Space-Want-Mac header.
func macCall(
	t *testing.T,
	handler http.HandlerFunc,
	params url.Values,
	wantMAC string,
) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/?"+params.Encode(), http.NoBody)
	if wantMAC != "" {
		req.Header.Set(spacecommit.WantMACHeader, wantMAC)
	}
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

// encapsulate negotiates a shared secret against kem's public key, returning
// the Want-Mac header to send for typ and the secret the response's MAC must
// be keyed by.
func encapsulate(t *testing.T, kem *atcrypto.PrivateKeyP256, typ string) (string, []byte) {
	t.Helper()
	pub, err := kem.PublicKey()
	require.NoError(t, err)
	keyShare, secret, err := spacecommit.Encapsulate(pub)
	require.NoError(t, err)
	header, err := spacecommit.FormatWantMAC(typ, keyShare)
	require.NoError(t, err)
	return header, secret
}

func TestServer_SpaceMACs(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	store := ts.SpaceStore
	// A second server over the same storage that authorizes every request
	// with a space credential, the way a syncer reads.
	credTS := pearserver_testutil.NewTestServer(t,
		pearserver_testutil.WithDB(ts.DB),
		pearserver_testutil.WithFGA(ts.FGA),
		pearserver_testutil.WithHive(ts.Hive),
		pearserver_testutil.WithHostKey(ts.HostKey),
		pearserver_testutil.WithSpaceStore(store),
		pearserver_testutil.WithValidator(authntest.NewSuccessValidator(&authn.CredentialInfo{
			Method: authn.ValidatorMethodSpaceCredential,
		})),
	)

	uri, err := store.CreateSpace(t.Context(), org, groupTp, "macs")
	require.NoError(t, err)
	_, recordCID, err := store.PutRecord(
		t.Context(), uri, owner, groupTp, "k1",
		spaces_testutil.MustMarshalRecord(t, map[string]any{"x": 1}),
	)
	require.NoError(t, err)

	// A hive-managed author's MACs are keyed by their own #atproto_kem.
	member, err := ts.Hive.MintIdentity(t.Context(), "macmember", "example.com")
	require.NoError(t, err)
	memberKEM, err := ts.Hive.KEMKeyForDID(t.Context(), member.DID)
	require.NoError(t, err)
	published, err := member.GetPublicKey(spacecommit.KEMFragment)
	require.NoError(t, err, "hive publishes the member's #atproto_kem")
	memberPub, err := memberKEM.PublicKey()
	require.NoError(t, err)
	require.True(t, memberPub.Equal(published))
	_, _, err = store.PutRecord(
		t.Context(), uri, member.DID, groupTp, "k1",
		spaces_testutil.MustMarshalRecord(t, map[string]any{"x": 2}),
	)
	require.NoError(t, err)

	repoParams := func(repo syntax.DID) url.Values {
		return url.Values{"space": {uri.String()}, "repo": {repo.String()}}
	}

	verifyCommit := func(t *testing.T, w *httptest.ResponseRecorder, repo syntax.DID, secret []byte, commit *habitat.NetworkHabitatSpaceDefsCommit) {
		t.Helper()
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Equal(t, spacecommit.WantMACHeader, w.Header().Get("Vary"))
		mac, err := spacecommit.ParseMAC(w.Header(), spacecommit.CommitMACType)
		require.NoError(t, err)
		require.NoError(t, spacecommit.FromXRPC(*commit).Verify(uri, repo, secret, mac))
	}

	for name, tc := range map[string]struct {
		repo syntax.DID
		kem  *atcrypto.PrivateKeyP256
	}{
		"external author, host kem key":  {owner, ts.HostKEMKey},
		"hive member, their own kem key": {member.DID, memberKEM},
	} {
		t.Run("getLatestCommit MACs the commit for "+name, func(t *testing.T) {
			header, secret := encapsulate(t, tc.kem, spacecommit.CommitMACType)
			w := macCall(t, credTS.Server.GetLatestCommit, repoParams(tc.repo), header)
			var out habitat.NetworkHabitatSpaceGetLatestCommitOutput
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
			verifyCommit(t, w, tc.repo, secret, out.Commit)
		})
	}

	t.Run("listRepoOps MACs the head commit", func(t *testing.T) {
		header, secret := encapsulate(t, ts.HostKEMKey, spacecommit.CommitMACType)
		w := macCall(t, credTS.Server.ListRepoOps, repoParams(owner), header)
		var out habitat.NetworkHabitatSpaceListRepoOpsOutput
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		verifyCommit(t, w, owner, secret, out.Commit)
	})

	t.Run("listRepoOps pages without a commit carry no MAC", func(t *testing.T) {
		header, _ := encapsulate(t, ts.HostKEMKey, spacecommit.CommitMACType)
		params := repoParams(owner)
		params.Set("limit", "1")
		w := macCall(t, credTS.Server.ListRepoOps, params, header)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var out habitat.NetworkHabitatSpaceListRepoOpsOutput
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		require.Nil(t, out.Commit)
		require.Empty(t, w.Header().Get(spacecommit.MACHeader))
	})

	t.Run("getRepo MACs the archive's commit", func(t *testing.T) {
		header, secret := encapsulate(t, ts.HostKEMKey, spacecommit.CommitMACType)
		w := macCall(t, credTS.Server.GetRepo, repoParams(owner), header)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		rev, data, _, err := store.RepoHead(t.Context(), uri, owner)
		require.NoError(t, err)
		mac, err := spacecommit.ParseMAC(w.Header(), spacecommit.CommitMACType)
		require.NoError(t, err)
		require.NoError(t, spacecommit.New(uri, owner, rev, data).Verify(uri, owner, secret, mac))
	})

	t.Run("getRecord MACs the record's URI and CID", func(t *testing.T) {
		header, secret := encapsulate(t, ts.HostKEMKey, spacecommit.RecordMACType)
		params := repoParams(owner)
		params.Set("collection", groupTp.String())
		params.Set("rkey", "k1")
		w := macCall(t, credTS.Server.GetRecord, params, header)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var out habitat.NetworkHabitatSpaceGetRecordOutput
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		require.Equal(t, recordCID.String(), out.Cid)
		mac, err := spacecommit.ParseMAC(w.Header(), spacecommit.RecordMACType)
		require.NoError(t, err)
		require.NoError(t, spacecommit.VerifyRecordMAC(
			secret,
			habitat_syntax.ConstructSpaceRecordURI(uri, owner, groupTp, "k1").String(),
			cid.MustParse(out.Cid),
			mac,
		))
	})

	t.Run("a space credential must request a MAC", func(t *testing.T) {
		wrongType, _ := encapsulate(t, ts.HostKEMKey, spacecommit.RecordMACType)
		for name, header := range map[string]string{
			"missing":    "",
			"malformed":  "not a dictionary (",
			"wrong type": wrongType,
			"bad key":    `type="atproto-space-repo", ver=1, key_share="did:key:zbad"`,
		} {
			t.Run(name, func(t *testing.T) {
				w := macCall(t, credTS.Server.GetLatestCommit, repoParams(owner), header)
				require.Equal(t, http.StatusBadRequest, w.Code)
				var apiErr atclient.ErrorBody
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &apiErr))
				require.Equal(t, "BadSpaceMac", apiErr.Name)
			})
		}
	})

	t.Run("an OAuth request may omit the MAC", func(t *testing.T) {
		w := macCall(t, ts.Server.GetLatestCommit, repoParams(owner), "")
		require.Equal(t, http.StatusOK, w.Code)
		require.Empty(t, w.Header().Get(spacecommit.MACHeader))

		header, secret := encapsulate(t, ts.HostKEMKey, spacecommit.CommitMACType)
		w = macCall(t, ts.Server.GetLatestCommit, repoParams(owner), header)
		var out habitat.NetworkHabitatSpaceGetLatestCommitOutput
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		verifyCommit(t, w, owner, secret, out.Commit)
	})
}
