package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/habitat-network/habitat/demos/fruitgang/internal/index"
	"github.com/habitat-network/habitat/demos/fruitgang/internal/server"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	store, err := index.NewStore(db)
	require.NoError(t, err)
	return server.New(store)
}

func TestHealthRoute(t *testing.T) {
	h := newTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]bool
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.True(t, body["ok"])
}

func TestGetMembersEmpty(t *testing.T) {
	h := newTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/getMembers", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var body []any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Empty(t, body)
}

func TestGetChatsReturnsData(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	store, _ := index.NewStore(db)
	_ = store.UpsertChat(index.Chat{URI: "ats://did:plc:a/community.fruitgang.chat/1", AuthorDID: "did:plc:a", Text: "yo", CreatedAt: time.Now()})

	h := server.New(store)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/getChats", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var body []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body, 1)
	require.Equal(t, "yo", body[0]["text"])
}

func TestGetRepliesFiltered(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	store, _ := index.NewStore(db)
	chatURI := "ats://did:plc:a/community.fruitgang.chat/1"
	_ = store.UpsertChatReply(index.ChatReply{URI: "ats://did:plc:a/community.fruitgang.chatReply/1", AuthorDID: "did:plc:a", ReplyTo: chatURI, Text: "great!", CreatedAt: time.Now()})
	_ = store.UpsertChatReply(index.ChatReply{URI: "ats://did:plc:a/community.fruitgang.chatReply/2", AuthorDID: "did:plc:a", ReplyTo: "other", Text: "nope", CreatedAt: time.Now()})

	h := server.New(store)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/getReplies?chatUri="+chatURI, nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var body []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body, 1)
	require.Equal(t, "great!", body[0]["text"])
}

func TestGetLogsReturnsData(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	store, _ := index.NewStore(db)
	_ = store.UpsertLog(index.Log{URI: "ats://did:plc:a/community.fruitgang.log/1", AuthorDID: "did:plc:a", Fruit: "community.fruitgang.log#banana", Count: 5, CreatedAt: time.Now()})

	h := server.New(store)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/getLogs", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var body []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body, 1)
	require.EqualValues(t, 5, body[0]["count"])
}
