package index_test

import (
	"testing"
	"time"

	"github.com/habitat-network/habitat/demos/fruitgang/internal/index"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestStore(t *testing.T) *index.Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	store, err := index.NewStore(db)
	require.NoError(t, err)
	return store
}

func TestUpsertAndGetMembers(t *testing.T) {
	store := newTestStore(t)
	m := index.Member{
		URI: "ats://did:plc:abc/community.fruitgang.member/1", DID: "did:plc:abc",
		DisplayName: "Alice", FavoriteFruit: "community.fruitgang.member#strawberry",
		CreatedAt: time.Now(),
	}
	require.NoError(t, store.UpsertMember(m))

	members, err := store.GetMembers()
	require.NoError(t, err)
	require.Len(t, members, 1)
	require.Equal(t, "Alice", members[0].DisplayName)
}

func TestUpsertMemberIsIdempotent(t *testing.T) {
	store := newTestStore(t)
	m := index.Member{URI: "ats://did:plc:abc/community.fruitgang.member/1", DID: "did:plc:abc", DisplayName: "Alice", CreatedAt: time.Now()}
	require.NoError(t, store.UpsertMember(m))
	m.DisplayName = "Alice Updated"
	require.NoError(t, store.UpsertMember(m))

	members, err := store.GetMembers()
	require.NoError(t, err)
	require.Len(t, members, 1)
	require.Equal(t, "Alice Updated", members[0].DisplayName)
}

func TestUpsertAndGetChats(t *testing.T) {
	store := newTestStore(t)
	c := index.Chat{URI: "ats://did:plc:abc/community.fruitgang.chat/1", AuthorDID: "did:plc:abc", Text: "hello fruits", CreatedAt: time.Now()}
	require.NoError(t, store.UpsertChat(c))

	chats, err := store.GetChats()
	require.NoError(t, err)
	require.Len(t, chats, 1)
	require.Equal(t, "hello fruits", chats[0].Text)
}

func TestGetRepliesFiltered(t *testing.T) {
	store := newTestStore(t)
	chatURI := "ats://did:plc:abc/community.fruitgang.chat/1"
	r1 := index.ChatReply{URI: "ats://did:plc:abc/community.fruitgang.chatReply/1", AuthorDID: "did:plc:abc", ReplyTo: chatURI, Text: "nice!", CreatedAt: time.Now()}
	r2 := index.ChatReply{URI: "ats://did:plc:abc/community.fruitgang.chatReply/2", AuthorDID: "did:plc:abc", ReplyTo: "other", Text: "other", CreatedAt: time.Now()}
	require.NoError(t, store.UpsertChatReply(r1))
	require.NoError(t, store.UpsertChatReply(r2))

	replies, err := store.GetReplies(chatURI)
	require.NoError(t, err)
	require.Len(t, replies, 1)
	require.Equal(t, "nice!", replies[0].Text)
}

func TestUpsertAndGetLogs(t *testing.T) {
	store := newTestStore(t)
	l := index.Log{URI: "ats://did:plc:abc/community.fruitgang.log/1", AuthorDID: "did:plc:abc", Fruit: "community.fruitgang.log#strawberry", Count: 3, CreatedAt: time.Now()}
	require.NoError(t, store.UpsertLog(l))

	logs, err := store.GetLogs()
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, 3, logs[0].Count)
}

func TestSetAndGetDefaultSpace(t *testing.T) {
	store := newTestStore(t)
	const orgDID = "did:plc:org"
	const spaceURI = "at://did:plc:org/network.habitat.space/abc123"

	// Not found before set
	_, err := store.GetDefaultSpaceURI(orgDID)
	require.ErrorIs(t, err, index.ErrNoDefaultSpace)

	require.NoError(t, store.SetDefaultSpace(orgDID, spaceURI))

	got, err := store.GetDefaultSpaceURI(orgDID)
	require.NoError(t, err)
	require.Equal(t, spaceURI, got)
}

func TestSetDefaultSpaceIsIdempotent(t *testing.T) {
	store := newTestStore(t)
	const orgDID = "did:plc:org"

	require.NoError(t, store.SetDefaultSpace(orgDID, "at://first"))
	require.NoError(t, store.SetDefaultSpace(orgDID, "at://second"))

	got, err := store.GetDefaultSpaceURI(orgDID)
	require.NoError(t, err)
	require.Equal(t, "at://second", got)
}
