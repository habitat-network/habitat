package foreignrepo

import (
	"encoding/hex"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	atgrp "github.com/habitat-network/habitat/api/atgrp"
	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/spacecommit"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	recipient = syntax.DID("did:plc:exampleforeign")
	key       = syntax.RecordKey("3lqswb6blws2b")
)

type fixture struct {
	db            *gorm.DB
	path          string
	host          hive.Hive
	ordinary      spaces.Store
	authored      *Store
	first, second syntax.DID
}

func openFixture(t *testing.T, path string) fixture {
	t.Helper()
	conn, err := db.New("sqlite://" + path)
	require.NoError(t, err)
	pool, err := conn.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	host, err := hive.NewHive("identity.example", "pear.example", conn)
	require.NoError(t, err)
	ordinary := spaces_testutil.NewTestStore(t, spaces_testutil.WithDB(conn))
	return fixture{
		db:       conn,
		path:     path,
		host:     host,
		ordinary: ordinary,
		authored: New(conn, host, ordinary),
	}
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	f := openFixture(t, filepath.Join(t.TempDir(), "author-repo.db"))
	require.NoError(t, db.Migrate(t.Context(), f.db, nil, hive.Models(), spaces.Models(), Models()))
	for i, name := range []string{"first", "second"} {
		id, err := f.host.MintOrgIdentity(t.Context(), name)
		require.NoError(t, err)
		if i == 0 {
			f.first = id.DID
		} else {
			f.second = id.DID
		}
	}
	return f
}

func inbox(owner syntax.DID) habitat_syntax.SpaceURI {
	return habitat_syntax.ConstructSpaceURI(owner, InboxType, "self")
}

func inviteURI(owner, author syntax.DID, rkey syntax.RecordKey) habitat_syntax.SpaceRecordURI {
	return habitat_syntax.ConstructSpaceRecordURI(inbox(owner), author, InviteCollection, rkey).
		AtSpace()
}

func invite(message string) *atgrp.OrgAtgrpInvite {
	return &atgrp.OrgAtgrpInvite{CreatedAt: "2026-11-27T08:00:00Z", Message: message}
}

func verifyAuth(
	t *testing.T, f fixture, c spacecommit.Commit, inbox habitat_syntax.SpaceURI, author syntax.DID,
) {
	t.Helper()
	priv, err := f.host.KEMKeyForDID(t.Context(), author)
	require.NoError(t, err)
	pub, err := priv.PublicKey()
	require.NoError(t, err)
	share, remoteSecret, err := spacecommit.Encapsulate(pub)
	require.NoError(t, err)
	localSecret, err := spacecommit.Decapsulate(priv, share)
	require.NoError(t, err)
	h := http.Header{}
	h.Set(
		spacecommit.AcceptSignatureHeader,
		spacecommit.FormatMACRequest(spacecommit.CommitMACType, share),
	)
	req, err := spacecommit.ParseMACRequest(h, spacecommit.CommitMACType)
	require.NoError(t, err)
	require.NotNil(t, req)
	require.NoError(t, spacecommit.SetCommitMAC(h, c, *req, localSecret))
	require.NoError(t, c.Verify(inbox, author, h, share, remoteSecret))
	require.Error(t, c.Verify(inbox, f.second, h, share, remoteSecret))
	wrongKey := make([]byte, len(remoteSecret))
	require.Error(t, c.Verify(inbox, author, h, share, wrongKey))
}

func TestConditionalWritesAndEmptyCommit(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	uri := inviteURI(recipient, f.first, key)
	initial, err := f.authored.Apply(ctx, uri, spaces.WriteCreate, invite("first"), "")
	require.NoError(t, err)
	snap, rows, err := f.authored.Snapshot(ctx, inbox(recipient).AtSpace(), f.first)
	require.NoError(t, err)
	require.Equal(t, initial, snap)
	require.Len(t, rows, 1)
	require.Equal(t, uri, rows[0].URI)
	require.Equal(t, f.first, snap.Repo)
	require.Equal(t, inbox(recipient).AtSpace(), snap.Space)
	oldCID := rows[0].CID.String()
	var expected spacecommit.LtHash
	expected.Add("org.atgrp.invite/3lqswb6blws2b/" + oldCID)
	require.Equal(t, expected.Sum(), snap.Data)
	verifyAuth(t, f, snap, inbox(recipient), f.first)

	_, err = f.authored.Apply(ctx, uri, spaces.WriteCreate, invite("duplicate"), "")
	require.ErrorIs(t, err, spaces.ErrRecordAlreadyExists)
	_, err = f.authored.Apply(ctx, uri, spaces.WriteUpdate, invite("bad"), "")
	require.ErrorIs(t, err, spaces.ErrInvalidRecord)
	same, err := f.authored.Apply(ctx, uri, spaces.WriteUpdate, invite("first"), oldCID)
	require.NoError(t, err)
	require.Equal(t, initial, same)
	updated, err := f.authored.Apply(ctx, uri, spaces.WriteUpdate, invite("second"), oldCID)
	require.NoError(t, err)
	require.Greater(t, updated.Rev, initial.Rev)
	_, err = f.authored.Apply(ctx, uri, spaces.WriteDelete, nil, oldCID)
	require.ErrorIs(t, err, ErrRecordChanged)
	live, err := f.authored.Get(ctx, uri)
	require.NoError(t, err)
	require.NotEqual(t, oldCID, live.CID.String())
	deleted, err := f.authored.Apply(ctx, uri, spaces.WriteDelete, nil, live.CID.String())
	require.NoError(t, err)
	require.Greater(t, deleted.Rev, updated.Rev)
	// Fixed all-zero LtHash SHA-256 vector in Proposal 0016, Repo lifecycle.
	require.Equal(t, "e5a00aa9991ac8a5ee3109844d84a55583bd20572ad3ffcd42792f3c36b183ad",
		hex.EncodeToString(deleted.Data))
	verifyAuth(t, f, deleted, inbox(recipient), f.first)
	_, err = f.authored.Get(ctx, uri)
	require.ErrorIs(t, err, spaces.ErrRecordNotFound)
	snap, rows, err = f.authored.Snapshot(ctx, inbox(recipient), f.first)
	require.NoError(t, err)
	require.Equal(t, deleted, snap)
	require.Empty(t, rows)
}

func TestRollbackAndRestart(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	uri := inviteURI(recipient, f.first, key)
	require.NoError(t, f.db.Exec(`CREATE TRIGGER reject_head BEFORE UPDATE ON foreign_invite_repos
		BEGIN SELECT RAISE(ABORT, 'reject head'); END`).Error)
	_, err := f.authored.Apply(ctx, uri, spaces.WriteCreate, invite("rollback"), "")
	require.ErrorContains(t, err, "reject head")
	_, err = f.authored.Get(ctx, uri)
	require.ErrorIs(t, err, spaces.ErrRecordNotFound)
	_, _, err = f.authored.Snapshot(ctx, inbox(recipient), f.first)
	require.ErrorIs(t, err, spaces.ErrRepoNotFound)
	require.NoError(t, f.db.Exec(`DROP TRIGGER reject_head`).Error)
	initial, err := f.authored.Apply(ctx, uri, spaces.WriteCreate, invite("original"), "")
	require.NoError(t, err)
	live, err := f.authored.Get(ctx, uri)
	require.NoError(t, err)
	refuse := errors.New("refuse outer commit")
	require.ErrorIs(t, f.db.Transaction(func(tx *gorm.DB) error {
		_, err := f.authored.WithTx(tx).Apply(ctx, uri, spaces.WriteDelete, nil, live.CID.String())
		require.NoError(t, err)
		return refuse
	}), refuse)
	pool, err := f.db.DB()
	require.NoError(t, err)
	require.NoError(t, pool.Close())

	reopened := openFixture(t, f.path)
	snap, rows, err := reopened.authored.Snapshot(ctx, inbox(recipient), f.first)
	require.NoError(t, err)
	require.Equal(t, initial, snap)
	require.Equal(t, []Record{live}, rows)
	// Simulate restarting with a wall clock behind the persisted high-water mark.
	ahead := syntax.NewTIDFromTime(time.Now().Add(time.Minute), 0)
	require.NoError(t, reopened.db.Exec(`UPDATE foreign_invite_repos SET rev = ?`, ahead).Error)
	next, err := reopened.authored.Apply(ctx, uri, spaces.WriteDelete, nil, live.CID.String())
	require.NoError(t, err)
	require.Greater(t, next.Rev, ahead.String())
	// A corrupted persisted head must not be returned as a verified snapshot.
	require.NoError(
		t,
		reopened.db.Exec(`UPDATE foreign_invite_repos SET state = ?`, []byte{1}).Error,
	)
	_, _, err = reopened.authored.Snapshot(ctx, inbox(recipient), f.first)
	require.ErrorIs(t, err, spacecommit.ErrInvalidCommit)
}

func TestScopeAndStorageIsolation(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	home, err := f.ordinary.CreateSpace(ctx, f.first, InboxType, "self")
	require.NoError(t, err)
	encoded, err := spaces.MarshalRecord(invite("local"))
	require.NoError(t, err)
	_, _, err = f.ordinary.PutRecord(ctx, home, f.first, InviteCollection, key, encoded)
	require.NoError(t, err)
	mirrorCID, err := recordPrefix.Sum(encoded)
	require.NoError(t, err)
	rev := syntax.NewTIDNow(0)
	require.NoError(t, f.ordinary.ApplyRemoteSync(ctx, home, recipient, spaces.RemoteSync{
		Rev: rev,
		Ops: []spaces.RemoteOp{
			{
				Collection: InviteCollection,
				Rkey:       key,
				Rev:        rev,
				Value:      encoded,
				Cid:        mirrorCID.String(),
			},
		},
	}))
	before, err := f.ordinary.SequenceEvents(ctx)
	require.NoError(t, err)
	for _, uri := range []habitat_syntax.SpaceRecordURI{
		inviteURI(f.first, f.first, key),
		inviteURI(f.first, f.second, key),
		inviteURI(recipient, recipient, key),
		inviteURI(recipient, "did:plc:unhostedauthor", key),
		inviteURI(recipient, f.first, "self"),
		habitat_syntax.ConstructSpaceRecordURI(inbox(recipient), f.first, "org.atgrp.membership", key),
		habitat_syntax.ConstructSpaceRecordURI(
			habitat_syntax.ConstructSpaceURI(recipient, InboxType, "other"), f.first, InviteCollection, key),
	} {
		_, err := f.authored.Apply(ctx, uri, spaces.WriteCreate, invite("reject"), "")
		require.ErrorIs(t, err, ErrScope, uri.String())
	}
	var count int64
	require.NoError(t, f.db.Model(&inviteRepo{}).Count(&count).Error)
	require.Zero(t, count)
	for _, author := range []syntax.DID{f.first, f.second} {
		uri := inviteURI(recipient, author, key)
		_, err := f.authored.Apply(
			ctx,
			uri,
			spaces.WriteCreate,
			invite("foreign "+author.String()),
			"",
		)
		require.NoError(t, err)
		snap, rows, err := f.authored.Snapshot(ctx, inbox(recipient), author)
		require.NoError(t, err)
		require.Equal(t, author, snap.Repo)
		require.Len(t, rows, 1)
		require.Equal(t, uri, rows[0].URI)
	}
	_, err = f.authored.Get(ctx, inviteURI("did:plc:anotherrecipient", f.first, key))
	require.ErrorIs(t, err, spaces.ErrRecordNotFound)
	exists, err := f.ordinary.CheckSpaceExists(ctx, inbox(recipient))
	require.NoError(t, err)
	require.False(t, exists)
	_, _, err = f.ordinary.RepoSnapshot(ctx, inbox(recipient), f.first)
	require.ErrorIs(t, err, spaces.ErrSpaceNotFound)
	after, err := f.ordinary.SequenceEvents(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)
	_, err = f.ordinary.GetRecord(ctx, home, f.first, InviteCollection, key)
	require.NoError(t, err)
	for _, table := range []string{"spaces", "space_repos", "space_records", "space_remote_records", "space_remote_syncs"} {
		require.NoError(t, f.db.Table(table).Count(&count).Error)
		require.EqualValues(t, 1, count, table)
	}
}

func TestWriteBoundsAndConcurrentFirstWrites(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	uri := inviteURI(recipient, f.first, key)
	_, err := f.authored.Apply(ctx, uri, spaces.WriteCreate, invite(strings.Repeat("x", 20001)), "")
	require.ErrorIs(t, err, spaces.ErrRecordTooLarge)
	_, err = f.authored.Apply(ctx, uri, spaces.WriteCreate, invite(strings.Repeat("x", 2001)), "")
	require.ErrorIs(t, err, spaces.ErrInvalidRecord)
	results := make(chan error, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			_, err := f.authored.Apply(ctx, uri, spaces.WriteCreate, invite("racing"), "")
			results <- err
		}()
	}
	close(start)
	first, second := <-results, <-results
	if first != nil {
		first, second = second, first
	}
	require.NoError(t, first)
	require.ErrorIs(t, second, spaces.ErrRecordAlreadyExists)

	for i := 1; i < maxRepoInvites; i++ {
		rkey := syntax.RecordKey(syntax.NewTIDFromTime(time.Unix(int64(i), 0), 0))
		_, err := f.authored.Apply(
			ctx,
			inviteURI(recipient, f.first, rkey),
			spaces.WriteCreate,
			invite("bounded"),
			"",
		)
		require.NoError(t, err)
	}
	extra := inviteURI(recipient, f.first, syntax.RecordKey(syntax.NewTIDNow(0)))
	_, err = f.authored.Apply(ctx, extra, spaces.WriteCreate, invite("over capacity"), "")
	require.ErrorIs(t, err, ErrRepoFull)
	_, rows, err := f.authored.Snapshot(ctx, inbox(recipient), f.first)
	require.NoError(t, err)
	require.Len(t, rows, maxRepoInvites)
}
