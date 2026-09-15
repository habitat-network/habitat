package pearserver_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/api/habitat"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
)

func TestSpaceBlobRegistrationFailure(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	space, err := ts.SpaceStore.CreateSpace(t.Context(), org, groupTp, "failure")
	require.NoError(t, err)
	injected := errors.New("synthetic registration failure")
	require.NoError(
		t,
		ts.DB.Callback().Create().Before("gorm:create").Register("comail-fault", func(tx *gorm.DB) {
			if tx.Statement.Table == "blob_uploads" {
				_ = tx.AddError(injected)
			}
		}),
	)
	raw := "synthetic bytes written before registration fails"
	upload := func() *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		ts.Server.UploadBlob(response, httptest.NewRequest(http.MethodPost,
			"/xrpc/network.habitat.repo.uploadBlob", strings.NewReader(raw)))
		return response
	}
	require.Equal(t, http.StatusInternalServerError, upload().Code)
	c, err := cid.NewPrefixV1(cid.Raw, multihash.SHA2_256).Sum([]byte(raw))
	require.NoError(t, err)
	read := httptest.NewRecorder()
	ts.Server.GetBlob(read, httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.getBlob?space="+url.QueryEscape(
			space.String(),
		)+"&cid="+c.String(),
		http.NoBody,
	))
	require.Equal(t, http.StatusNotFound, read.Code)
	require.NoError(t, ts.DB.Callback().Create().Remove("comail-fault"))
	retry := upload()
	require.Equal(t, http.StatusOK, retry.Code)
	var blob habitat.NetworkHabitatRepoUploadBlobOutput
	require.NoError(t, json.Unmarshal(retry.Body.Bytes(), &blob))
	_, _, err = ts.SpaceStore.PutRecord(
		t.Context(),
		space,
		owner,
		groupTp,
		"retry",
		spaces_testutil.MustMarshalRecord(
			t,
			map[string]any{"$type": groupTp.String(), "blob": blob.Blob},
		),
	)
	require.NoError(t, err)
	read = httptest.NewRecorder()
	ts.Server.GetBlob(read, httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.getBlob?space="+url.QueryEscape(
			space.String(),
		)+"&cid="+c.String(),
		http.NoBody,
	))
	require.Equal(t, http.StatusOK, read.Code)
	require.Equal(t, raw, read.Body.String())
}

func TestSpaceBlobRecordReferenceRollback(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	space, err := ts.SpaceStore.CreateSpace(t.Context(), org, groupTp, "rollback")
	require.NoError(t, err)
	response := httptest.NewRecorder()
	ts.Server.UploadBlob(response, httptest.NewRequest(http.MethodPost,
		"/xrpc/network.habitat.repo.uploadBlob", strings.NewReader("synthetic transaction bytes")))
	require.Equal(t, http.StatusOK, response.Code)
	var blob habitat.NetworkHabitatRepoUploadBlobOutput
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &blob))
	value := spaces_testutil.MustMarshalRecord(
		t,
		map[string]any{"$type": groupTp.String(), "blobs": []any{blob.Blob, blob.Blob}},
	)
	injected := errors.New("synthetic reference insertion failure")
	require.NoError(
		t,
		ts.DB.Callback().Create().Before("gorm:create").Register("comail-fault", func(tx *gorm.DB) {
			if tx.Statement.Table == "space_blob_refs" {
				_ = tx.AddError(injected)
			}
		}),
	)
	_, _, err = ts.SpaceStore.PutRecord(t.Context(), space, owner, groupTp, "retry", value)
	require.ErrorIs(t, err, injected)
	_, err = ts.SpaceStore.GetRecord(t.Context(), space, owner, groupTp, "retry")
	require.ErrorIs(t, err, spaces.ErrRecordNotFound)
	c, err := cid.Parse(blob.Cid)
	require.NoError(t, err)
	referenced, err := ts.SpaceStore.SpaceReferencesBlob(t.Context(), space, c)
	require.NoError(t, err)
	require.False(t, referenced)
	require.NoError(t, ts.DB.Callback().Create().Remove("comail-fault"))
	_, firstCID, err := ts.SpaceStore.PutRecord(t.Context(), space, owner, groupTp, "retry", value)
	require.NoError(t, err)
	_, retryCID, err := ts.SpaceStore.PutRecord(t.Context(), space, owner, groupTp, "retry", value)
	require.NoError(t, err)
	require.Equal(t, firstCID, retryCID)
	referenced, err = ts.SpaceStore.SpaceReferencesBlob(t.Context(), space, c)
	require.NoError(t, err)
	require.True(t, referenced)
	require.NoError(t, ts.SpaceStore.DeleteSpace(t.Context(), space))
	referenced, err = ts.SpaceStore.SpaceReferencesBlob(t.Context(), space, c)
	require.NoError(t, err)
	require.False(t, referenced)
}
