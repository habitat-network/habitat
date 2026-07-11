package sap

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/api/habitat"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
)

// fakeAuth is a serviceAuthValidator that returns a fixed issuer DID, recording
// the lexicon method it was asked to validate.
type fakeAuth struct {
	issuer  syntax.DID
	lastLxm syntax.NSID
}

func (f *fakeAuth) Validate(_ context.Context, _ string, lxm *syntax.NSID) (syntax.DID, error) {
	if lxm != nil {
		f.lastLxm = *lxm
	}
	return f.issuer, nil
}

func newNotifyTest(t *testing.T, issuer syntax.DID) (*notifyServer, *gorm.DB, *utils.PollNotifier) {
	t.Helper()
	db := openTestDB(t)
	resyncNotif := utils.NewPollNotifier()
	outboxNotif := utils.NewPollNotifier()
	resyncBuf := newResyncBuffer(db, resyncNotif, outboxNotif)
	r := newResyncer(db, nil, resyncBuf, resyncNotif, outboxNotif, 1, newTestMetrics(t))
	n := newNotifyServer(db, r, &fakeAuth{issuer: issuer})
	return n, db, resyncNotif
}

func postJSON(t *testing.T, handler http.HandlerFunc, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

// TestNotifyServer_NotifyWriteRequeuesActiveRepo checks that a notifyWrite for a
// repo that has fallen behind the notified rev returns it to a claimable state
// and wakes the dispatcher.
func TestNotifyServer_NotifyWriteRequeuesActiveRepo(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	repo := syntax.DID("did:plc:member1")
	n, db, resyncNotif := newNotifyTest(t, space.SpaceOwner())

	require.NoError(t, db.Create(&managedRepo{
		Space: space, DID: repo, State: RepoStateActive, Rev: syntax.TID("aaa"),
	}).Error)

	notified := resyncNotif.Listen()
	w := postJSON(t, n.HandleNotifyWrite, "/xrpc/network.habitat.space.notifyWrite",
		habitat.NetworkHabitatSpaceNotifyWriteInput{
			Space: space.String(), Repo: repo.String(), Rev: "bbb", Hash: "",
		})
	require.Equal(t, http.StatusOK, w.Code)

	var got managedRepo
	require.NoError(t, db.Where("space = ? AND did = ?", space, repo).First(&got).Error)
	require.Equal(t, RepoStatePending, got.State)

	select {
	case <-notified:
	default:
		t.Fatal("expected the resync dispatcher to be notified")
	}
}

// TestNotifyServer_NotifyWriteTracksNewRepo checks that a notifyWrite for a repo
// the syncer has never seen starts tracking it as pending.
func TestNotifyServer_NotifyWriteTracksNewRepo(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	repo := syntax.DID("did:plc:member2")
	n, db, _ := newNotifyTest(t, space.SpaceOwner())

	w := postJSON(t, n.HandleNotifyWrite, "/xrpc/network.habitat.space.notifyWrite",
		habitat.NetworkHabitatSpaceNotifyWriteInput{
			Space: space.String(), Repo: repo.String(), Rev: "aaa", Hash: "",
		})
	require.Equal(t, http.StatusOK, w.Code)

	var got managedRepo
	require.NoError(t, db.Where("space = ? AND did = ?", space, repo).First(&got).Error)
	require.Equal(t, RepoStatePending, got.State)
}

// TestNotifyServer_RejectsWrongIssuer checks that a notification signed by
// someone other than the space authority is rejected.
func TestNotifyServer_RejectsWrongIssuer(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	repo := syntax.DID("did:plc:member1")
	// Issuer is not the space owner.
	n, db, _ := newNotifyTest(t, syntax.DID("did:plc:someoneelse"))

	w := postJSON(t, n.HandleNotifyWrite, "/xrpc/network.habitat.space.notifyWrite",
		habitat.NetworkHabitatSpaceNotifyWriteInput{
			Space: space.String(), Repo: repo.String(), Rev: "aaa", Hash: "",
		})
	require.Equal(t, http.StatusBadRequest, w.Code)

	var count int64
	require.NoError(t, db.Model(&managedRepo{}).Count(&count).Error)
	require.Zero(t, count, "no repo should be tracked for an unauthorized notify")
}

// TestNotifyServer_NotifySpaceDeletedForgetsSpace checks that notifySpaceDeleted
// drops the syncer's tracking state for the space.
func TestNotifyServer_NotifySpaceDeletedForgetsSpace(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	other := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s2")
	n, db, _ := newNotifyTest(t, space.SpaceOwner())

	require.NoError(t, db.Create(&managedRepo{Space: space, DID: "did:plc:m1", State: RepoStateActive}).Error)
	require.NoError(t, db.Create(&managedSpace{Space: space, DID: "did:plc:owner"}).Error)
	require.NoError(t, db.Create(&bufferedEvent{Space: space, DID: "did:plc:m1", Seq: 1, Data: []byte("{}")}).Error)
	// A second space must be left untouched.
	require.NoError(t, db.Create(&managedRepo{Space: other, DID: "did:plc:m1", State: RepoStateActive}).Error)

	w := postJSON(t, n.HandleNotifySpaceDeleted, "/xrpc/network.habitat.space.notifySpaceDeleted",
		habitat.NetworkHabitatSpaceNotifySpaceDeletedInput{Space: space.String()})
	require.Equal(t, http.StatusOK, w.Code)

	var repos, spaces, events int64
	require.NoError(t, db.Model(&managedRepo{}).Where("space = ?", space).Count(&repos).Error)
	require.NoError(t, db.Model(&managedSpace{}).Where("space = ?", space).Count(&spaces).Error)
	require.NoError(t, db.Model(&bufferedEvent{}).Where("space = ?", space).Count(&events).Error)
	require.Zero(t, repos)
	require.Zero(t, spaces)
	require.Zero(t, events)

	var otherCount int64
	require.NoError(t, db.Model(&managedRepo{}).Where("space = ?", other).Count(&otherCount).Error)
	require.Equal(t, int64(1), otherCount, "unrelated space must be preserved")
}
