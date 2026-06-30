# Fruit Gang Demo App — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a local demo app at `demos/fruitgang/` that showcases Habitat's community-space capabilities through a playful fruit-themed community with member profiles, chats, and fruit logging.

**Architecture:** A Go backend at `demos/fruitgang/backend/` (separate Go module) embeds SAP for indexing AT Protocol records from org members' repos into a local SQLite DB, and exposes read endpoints (`/getMembers`, `/getChats`, `/getReplies`, `/getLogs`). A TypeScript/React frontend at `demos/fruitgang/frontend/` uses `AuthManager` from `internal` for OAuth, reads from the fruitgang backend, and writes to pear directly.

**Tech Stack:** Go 1.26, GORM + SQLite, `github.com/urfave/cli/v3`, `golang.org/x/sync`; React 19, TanStack Router + Query, Tailwind CSS 4, Original Surfer + Quicksand (Google Fonts), Vite 7.

## Global Constraints

- All new files live under `demos/fruitgang/` — never modify files outside this dir except Caddyfile, `.moon/workspace.yml`, `pnpm-workspace.yaml`
- Backend module name: `github.com/habitat-network/habitat/demos/fruitgang`
- Backend replaces root module: `replace github.com/habitat-network/habitat => ../../..`
- Backend port: `3100`; frontend port: `5178`
- Frontend domain: `fruitgang.local.habitat.network`; backend domain: `fruitgang-api.local.habitat.network`
- All CSS custom properties defined only in `src/theme.css`; no hardcoded color values in components
- Fruit metadata (emoji, label, CSS var) lives only in `src/fruits.ts`
- Writes go to pear via `procedure("network.habitat.repo.putRecord", ...)` from `internal`
- Reads go to the fruitgang backend (`__FRUITGANG_API__` injected by vite config)
- No pagination, no real-time push, no avatar upload UI, no post editing/deletion

---

## File Map

**New files (backend):**
- `demos/fruitgang/backend/go.mod` — separate module declaration + replace directive
- `demos/fruitgang/backend/cmd/main.go` — entrypoint: CLI flags, wires SAP + indexer + HTTP server
- `demos/fruitgang/backend/internal/index/models.go` — GORM models for 4 index tables
- `demos/fruitgang/backend/internal/index/store.go` — upsert + query helpers for all tables
- `demos/fruitgang/backend/internal/index/store_test.go` — unit tests for store
- `demos/fruitgang/backend/internal/indexer/indexer.go` — drains SAP outbox → upserts into index
- `demos/fruitgang/backend/internal/server/server.go` — HTTP handlers for all routes
- `demos/fruitgang/backend/internal/server/server_test.go` — httptest tests for routes
- `demos/fruitgang/backend/moon.yml` — overrides `dev` task for separate module

**New files (lexicons):**
- `demos/fruitgang/lexicons/community/fruitgang/member.json`
- `demos/fruitgang/lexicons/community/fruitgang/chat.json`
- `demos/fruitgang/lexicons/community/fruitgang/chatReply.json`
- `demos/fruitgang/lexicons/community/fruitgang/log.json`

**New files (frontend):**
- `demos/fruitgang/frontend/package.json`
- `demos/fruitgang/frontend/tsconfig.json`
- `demos/fruitgang/frontend/vite.config.ts`
- `demos/fruitgang/frontend/index.html`
- `demos/fruitgang/frontend/moon.yml`
- `demos/fruitgang/frontend/src/globals.d.ts` — `__DOMAIN__`, `__HABITAT_DOMAIN__`, `__FRUITGANG_API__` declarations
- `demos/fruitgang/frontend/src/theme.css` — all tokens: palette, fonts, Tailwind `@theme`
- `demos/fruitgang/frontend/src/fruits.ts` — `FRUITS` map: slug → `{emoji, label, colorVar}`
- `demos/fruitgang/frontend/src/main.tsx` — router + query client setup
- `demos/fruitgang/frontend/src/routes/__root.tsx` — root route with router context
- `demos/fruitgang/frontend/src/routes/login.tsx` — login page using `AuthForm`
- `demos/fruitgang/frontend/src/routes/_app.tsx` — auth guard + persistent nav layout
- `demos/fruitgang/frontend/src/routes/_app/members.tsx` — member grid + profile setup
- `demos/fruitgang/frontend/src/routes/_app/chats.tsx` — chat feed + inline replies
- `demos/fruitgang/frontend/src/routes/_app/log.tsx` — fruit log feed + compose form

**New files (workspace root):**
- `demos/fruitgang/moon.yml` — convenience `dev` alias that starts both backend and frontend

**Modified files:**
- `.moon/workspace.yml` — add `fruitgang`, `fruitgang-backend`, `fruitgang-frontend` sources
- `pnpm-workspace.yaml` — add `demos/fruitgang/frontend`
- `Caddyfile` — add two entries for the new subdomains

---

### Task 1: Workspace scaffolding + lexicons

**Files:**
- Modify: `.moon/workspace.yml`
- Modify: `pnpm-workspace.yaml`
- Modify: `Caddyfile`
- Create: `demos/fruitgang/moon.yml`
- Create: `demos/fruitgang/lexicons/community/fruitgang/member.json`
- Create: `demos/fruitgang/lexicons/community/fruitgang/chat.json`
- Create: `demos/fruitgang/lexicons/community/fruitgang/chatReply.json`
- Create: `demos/fruitgang/lexicons/community/fruitgang/log.json`

**Interfaces:**
- Produces: Moon project IDs `fruitgang`, `fruitgang-backend`, `fruitgang-frontend`; lexicon collection NSIDs `community.fruitgang.member`, `community.fruitgang.chat`, `community.fruitgang.chatReply`, `community.fruitgang.log`

- [ ] **Step 1: Create directory structure**

```bash
mkdir -p demos/fruitgang/backend/cmd
mkdir -p demos/fruitgang/backend/internal/{index,indexer,server}
mkdir -p demos/fruitgang/frontend/src/routes/_app
mkdir -p demos/fruitgang/lexicons/community/fruitgang
```

- [ ] **Step 2: Register Moon projects**

In `.moon/workspace.yml`, add to the `sources` block (keeping all existing entries):

```yaml
    fruitgang: demos/fruitgang
    fruitgang-backend: demos/fruitgang/backend
    fruitgang-frontend: demos/fruitgang/frontend
```

- [ ] **Step 3: Register frontend in pnpm workspace**

In `pnpm-workspace.yaml`, add to the `packages` list:

```yaml
  - demos/fruitgang/frontend
```

- [ ] **Step 4: Add Caddyfile entries**

Append to `Caddyfile`:

```
fruitgang.local.habitat.network {
  reverse_proxy 127.0.0.1:5178
  tls internal
}

fruitgang-api.local.habitat.network {
  reverse_proxy 127.0.0.1:3100
  tls internal
}
```

- [ ] **Step 5: Create fruitgang root moon.yml**

```yaml
# demos/fruitgang/moon.yml
tasks:
  dev:
    command: noop
    deps:
      - fruitgang-backend:dev
      - fruitgang-frontend:dev
    preset: server
```

- [ ] **Step 6: Write member lexicon**

```json
// demos/fruitgang/lexicons/community/fruitgang/member.json
{
  "lexicon": 1,
  "id": "community.fruitgang.member",
  "defs": {
    "main": {
      "type": "record",
      "description": "A Fruit Gang member profile.",
      "key": "tid",
      "record": {
        "type": "object",
        "required": ["displayName", "createdAt"],
        "properties": {
          "displayName": { "type": "string", "maxLength": 64 },
          "avatar": { "type": "blob" },
          "funFact": { "type": "string", "maxLength": 280 },
          "favoriteFruit": {
            "type": "string",
            "knownValues": [
              "community.fruitgang.member#greenApple",
              "community.fruitgang.member#redApple",
              "community.fruitgang.member#pear",
              "community.fruitgang.member#tangerine",
              "community.fruitgang.member#lemon",
              "community.fruitgang.member#lime",
              "community.fruitgang.member#banana",
              "community.fruitgang.member#watermelon",
              "community.fruitgang.member#grapes",
              "community.fruitgang.member#strawberry",
              "community.fruitgang.member#blueberry",
              "community.fruitgang.member#melon",
              "community.fruitgang.member#cherries",
              "community.fruitgang.member#peach",
              "community.fruitgang.member#mango",
              "community.fruitgang.member#pineapple",
              "community.fruitgang.member#coconut",
              "community.fruitgang.member#kiwi",
              "community.fruitgang.member#tomato",
              "community.fruitgang.member#olive",
              "community.fruitgang.member#avocado"
            ]
          },
          "createdAt": { "type": "string", "format": "datetime" }
        }
      }
    },
    "greenApple": { "type": "token" },
    "redApple": { "type": "token" },
    "pear": { "type": "token" },
    "tangerine": { "type": "token" },
    "lemon": { "type": "token" },
    "lime": { "type": "token" },
    "banana": { "type": "token" },
    "watermelon": { "type": "token" },
    "grapes": { "type": "token" },
    "strawberry": { "type": "token" },
    "blueberry": { "type": "token" },
    "melon": { "type": "token" },
    "cherries": { "type": "token" },
    "peach": { "type": "token" },
    "mango": { "type": "token" },
    "pineapple": { "type": "token" },
    "coconut": { "type": "token" },
    "kiwi": { "type": "token" },
    "tomato": { "type": "token" },
    "olive": { "type": "token" },
    "avocado": { "type": "token" }
  }
}
```

- [ ] **Step 7: Write chat lexicon**

```json
// demos/fruitgang/lexicons/community/fruitgang/chat.json
{
  "lexicon": 1,
  "id": "community.fruitgang.chat",
  "defs": {
    "main": {
      "type": "record",
      "description": "A top-level Fruit Gang community post.",
      "key": "tid",
      "record": {
        "type": "object",
        "required": ["text", "createdAt"],
        "properties": {
          "text": { "type": "string", "maxLength": 300 },
          "createdAt": { "type": "string", "format": "datetime" }
        }
      }
    }
  }
}
```

- [ ] **Step 8: Write chatReply lexicon**

```json
// demos/fruitgang/lexicons/community/fruitgang/chatReply.json
{
  "lexicon": 1,
  "id": "community.fruitgang.chatReply",
  "defs": {
    "main": {
      "type": "record",
      "description": "A flat reply to a community.fruitgang.chat record.",
      "key": "tid",
      "record": {
        "type": "object",
        "required": ["text", "replyTo", "createdAt"],
        "properties": {
          "text": { "type": "string", "maxLength": 300 },
          "replyTo": { "type": "string", "format": "at-uri" },
          "createdAt": { "type": "string", "format": "datetime" }
        }
      }
    }
  }
}
```

- [ ] **Step 9: Write log lexicon**

```json
// demos/fruitgang/lexicons/community/fruitgang/log.json
{
  "lexicon": 1,
  "id": "community.fruitgang.log",
  "defs": {
    "main": {
      "type": "record",
      "description": "A Fruit Gang fruit log entry.",
      "key": "tid",
      "record": {
        "type": "object",
        "required": ["fruit", "count", "createdAt"],
        "properties": {
          "fruit": {
            "type": "string",
            "knownValues": [
              "community.fruitgang.log#greenApple",
              "community.fruitgang.log#redApple",
              "community.fruitgang.log#pear",
              "community.fruitgang.log#tangerine",
              "community.fruitgang.log#lemon",
              "community.fruitgang.log#lime",
              "community.fruitgang.log#banana",
              "community.fruitgang.log#watermelon",
              "community.fruitgang.log#grapes",
              "community.fruitgang.log#strawberry",
              "community.fruitgang.log#blueberry",
              "community.fruitgang.log#melon",
              "community.fruitgang.log#cherries",
              "community.fruitgang.log#peach",
              "community.fruitgang.log#mango",
              "community.fruitgang.log#pineapple",
              "community.fruitgang.log#coconut",
              "community.fruitgang.log#kiwi",
              "community.fruitgang.log#tomato",
              "community.fruitgang.log#olive",
              "community.fruitgang.log#avocado"
            ]
          },
          "count": { "type": "integer", "minimum": 1, "maximum": 99 },
          "createdAt": { "type": "string", "format": "datetime" }
        }
      }
    },
    "greenApple": { "type": "token" },
    "redApple": { "type": "token" },
    "pear": { "type": "token" },
    "tangerine": { "type": "token" },
    "lemon": { "type": "token" },
    "lime": { "type": "token" },
    "banana": { "type": "token" },
    "watermelon": { "type": "token" },
    "grapes": { "type": "token" },
    "strawberry": { "type": "token" },
    "blueberry": { "type": "token" },
    "melon": { "type": "token" },
    "cherries": { "type": "token" },
    "peach": { "type": "token" },
    "mango": { "type": "token" },
    "pineapple": { "type": "token" },
    "coconut": { "type": "token" },
    "kiwi": { "type": "token" },
    "tomato": { "type": "token" },
    "olive": { "type": "token" },
    "avocado": { "type": "token" }
  }
}
```

- [ ] **Step 10: Commit**

```bash
git add .moon/workspace.yml pnpm-workspace.yaml Caddyfile demos/fruitgang/
git commit -m "chore: scaffold Fruit Gang demo app workspace + lexicons"
```

---

### Task 2: Backend Go module + index models + store

**Files:**
- Create: `demos/fruitgang/backend/go.mod`
- Create: `demos/fruitgang/backend/internal/index/models.go`
- Create: `demos/fruitgang/backend/internal/index/store.go`
- Create: `demos/fruitgang/backend/internal/index/store_test.go`

**Interfaces:**
- Produces:
  - `index.Member`, `index.Chat`, `index.ChatReply`, `index.Log` GORM model structs
  - `index.Store` interface with methods: `UpsertMember(Member) error`, `UpsertChat(Chat) error`, `UpsertChatReply(ChatReply) error`, `UpsertLog(Log) error`, `GetMembers() ([]Member, error)`, `GetChats() ([]Chat, error)`, `GetReplies(chatURI string) ([]ChatReply, error)`, `GetLogs() ([]Log, error)`
  - `index.NewStore(db *gorm.DB) (*Store, error)` — runs AutoMigrate and returns store

- [ ] **Step 1: Write go.mod**

```
// demos/fruitgang/backend/go.mod
module github.com/habitat-network/habitat/demos/fruitgang

go 1.26.3

replace github.com/habitat-network/habitat => ../../..

require (
	github.com/habitat-network/habitat v0.0.0
	github.com/urfave/cli/v3 v3.3.3
	golang.org/x/sync v0.20.0
	gorm.io/driver/sqlite v1.6.0
	gorm.io/gorm v1.30.0
)
```

- [ ] **Step 2: Run go mod tidy to resolve all transitive deps**

```bash
cd demos/fruitgang/backend && go mod tidy
```

Expected: `go.sum` is generated; no errors.

- [ ] **Step 3: Write index models**

```go
// demos/fruitgang/backend/internal/index/models.go
package index

import "time"

type Member struct {
	URI           string    `gorm:"primaryKey"`
	DID           string    `gorm:"index"`
	DisplayName   string
	AvatarCID     string
	FunFact       string
	FavoriteFruit string
	CreatedAt     time.Time
	IndexedAt     time.Time
}

type Chat struct {
	URI       string    `gorm:"primaryKey"`
	AuthorDID string    `gorm:"index"`
	Text      string
	CreatedAt time.Time
	IndexedAt time.Time
}

type ChatReply struct {
	URI       string    `gorm:"primaryKey"`
	AuthorDID string    `gorm:"index"`
	ReplyTo   string    `gorm:"index"`
	Text      string
	CreatedAt time.Time
	IndexedAt time.Time
}

type Log struct {
	URI       string    `gorm:"primaryKey"`
	AuthorDID string    `gorm:"index"`
	Fruit     string
	Count     int
	CreatedAt time.Time
	IndexedAt time.Time
}
```

- [ ] **Step 4: Write failing store test**

```go
// demos/fruitgang/backend/internal/index/store_test.go
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
```

- [ ] **Step 5: Run tests to confirm they fail**

```bash
cd demos/fruitgang/backend && go test ./internal/index/...
```

Expected: FAIL — `index.NewStore` and `index.Store` undefined.

- [ ] **Step 6: Write store implementation**

```go
// demos/fruitgang/backend/internal/index/store.go
package index

import (
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Store struct {
	db *gorm.DB
}

func NewStore(db *gorm.DB) (*Store, error) {
	if err := db.AutoMigrate(&Member{}, &Chat{}, &ChatReply{}, &Log{}); err != nil {
		return nil, fmt.Errorf("migrate index tables: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) UpsertMember(m Member) error {
	m.IndexedAt = time.Now()
	return s.db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&m).Error
}

func (s *Store) UpsertChat(c Chat) error {
	c.IndexedAt = time.Now()
	return s.db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&c).Error
}

func (s *Store) UpsertChatReply(r ChatReply) error {
	r.IndexedAt = time.Now()
	return s.db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&r).Error
}

func (s *Store) UpsertLog(l Log) error {
	l.IndexedAt = time.Now()
	return s.db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&l).Error
}

func (s *Store) GetMembers() ([]Member, error) {
	var out []Member
	return out, s.db.Order("created_at ASC").Find(&out).Error
}

func (s *Store) GetChats() ([]Chat, error) {
	var out []Chat
	return out, s.db.Order("created_at DESC").Find(&out).Error
}

func (s *Store) GetReplies(chatURI string) ([]ChatReply, error) {
	var out []ChatReply
	return out, s.db.Where("reply_to = ?", chatURI).Order("created_at ASC").Find(&out).Error
}

func (s *Store) GetLogs() ([]Log, error) {
	var out []Log
	return out, s.db.Order("created_at DESC").Find(&out).Error
}
```

- [ ] **Step 7: Run tests to confirm they pass**

```bash
cd demos/fruitgang/backend && go test ./internal/index/...
```

Expected: PASS — all 5 tests green.

- [ ] **Step 8: Commit**

```bash
git add demos/fruitgang/backend/
git commit -m "feat(fruitgang): backend Go module + index models + store"
```

---

### Task 3: Backend indexer

**Files:**
- Create: `demos/fruitgang/backend/internal/indexer/indexer.go`

**Interfaces:**
- Consumes: `index.Store` (all Upsert methods); `sap.Outbox` (`Poll`, `Ack`, `Watch`); `habitat_syntax.SpaceRecordURI` (`.Collection()`, `.Repo()`, `.String()`)
- Produces: `indexer.Run(ctx context.Context, outbox sap.Outbox, store *index.Store) error` — blocking goroutine, returns when ctx is cancelled

- [ ] **Step 1: Write indexer**

```go
// demos/fruitgang/backend/internal/indexer/indexer.go
package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/habitat-network/habitat/demos/fruitgang/internal/index"
	"github.com/habitat-network/habitat/internal/sap"
)

type memberRecord struct {
	DisplayName   string `json:"displayName"`
	FunFact       string `json:"funFact"`
	FavoriteFruit string `json:"favoriteFruit"`
	CreatedAt     string `json:"createdAt"`
}

type chatRecord struct {
	Text      string `json:"text"`
	CreatedAt string `json:"createdAt"`
}

type chatReplyRecord struct {
	Text      string `json:"text"`
	ReplyTo   string `json:"replyTo"`
	CreatedAt string `json:"createdAt"`
}

type logRecord struct {
	Fruit     string `json:"fruit"`
	Count     int    `json:"count"`
	CreatedAt string `json:"createdAt"`
}

func Run(ctx context.Context, outbox sap.Outbox, store *index.Store) error {
	watch := outbox.Watch()
	for {
		msgs, err := outbox.Poll(ctx, 50)
		if err != nil {
			return fmt.Errorf("poll outbox: %w", err)
		}
		for _, msg := range msgs {
			if err := process(msg, store); err != nil {
				slog.WarnContext(ctx, "skipping unprocessable outbox message", "uri", msg.URI, "err", err)
			}
			if err := outbox.Ack(ctx, msg.ID); err != nil {
				return fmt.Errorf("ack outbox message %d: %w", msg.ID, err)
			}
		}
		if len(msgs) == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-watch:
			case <-time.After(5 * time.Second):
			}
		}
	}
}

func process(msg sap.OutboxMessage, store *index.Store) error {
	collection := string(msg.URI.Collection())
	did := string(msg.URI.Repo())
	uri := msg.URI.String()

	switch collection {
	case "community.fruitgang.member":
		var r memberRecord
		if err := json.Unmarshal(msg.Value, &r); err != nil {
			return fmt.Errorf("decode member: %w", err)
		}
		t, _ := time.Parse(time.RFC3339, r.CreatedAt)
		return store.UpsertMember(index.Member{
			URI: uri, DID: did, DisplayName: r.DisplayName,
			FunFact: r.FunFact, FavoriteFruit: r.FavoriteFruit, CreatedAt: t,
		})

	case "community.fruitgang.chat":
		var r chatRecord
		if err := json.Unmarshal(msg.Value, &r); err != nil {
			return fmt.Errorf("decode chat: %w", err)
		}
		t, _ := time.Parse(time.RFC3339, r.CreatedAt)
		return store.UpsertChat(index.Chat{URI: uri, AuthorDID: did, Text: r.Text, CreatedAt: t})

	case "community.fruitgang.chatReply":
		var r chatReplyRecord
		if err := json.Unmarshal(msg.Value, &r); err != nil {
			return fmt.Errorf("decode chatReply: %w", err)
		}
		t, _ := time.Parse(time.RFC3339, r.CreatedAt)
		return store.UpsertChatReply(index.ChatReply{
			URI: uri, AuthorDID: did, ReplyTo: r.ReplyTo, Text: r.Text, CreatedAt: t,
		})

	case "community.fruitgang.log":
		var r logRecord
		if err := json.Unmarshal(msg.Value, &r); err != nil {
			return fmt.Errorf("decode log: %w", err)
		}
		t, _ := time.Parse(time.RFC3339, r.CreatedAt)
		return store.UpsertLog(index.Log{
			URI: uri, AuthorDID: did, Fruit: r.Fruit, Count: r.Count, CreatedAt: t,
		})

	default:
		// Not a fruitgang record — ignore silently
		return nil
	}
}
```

- [ ] **Step 2: Verify it compiles**

```bash
cd demos/fruitgang/backend && go build ./internal/indexer/...
```

Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
git add demos/fruitgang/backend/internal/indexer/
git commit -m "feat(fruitgang): backend outbox indexer goroutine"
```

---

### Task 4: Backend HTTP server

**Files:**
- Create: `demos/fruitgang/backend/internal/server/server.go`
- Create: `demos/fruitgang/backend/internal/server/server_test.go`

**Interfaces:**
- Consumes: `index.Store` (all Get* methods)
- Produces: `server.New(store *index.Store) http.Handler` — returns an `http.ServeMux` with all routes registered

- [ ] **Step 1: Write failing server test**

```go
// demos/fruitgang/backend/internal/server/server_test.go
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
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd demos/fruitgang/backend && go test ./internal/server/...
```

Expected: FAIL — `server.New` undefined.

- [ ] **Step 3: Write server implementation**

```go
// demos/fruitgang/backend/internal/server/server.go
package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/habitat-network/habitat/demos/fruitgang/internal/index"
)

type memberResponse struct {
	URI           string    `json:"uri"`
	DID           string    `json:"did"`
	DisplayName   string    `json:"displayName"`
	AvatarCID     string    `json:"avatarCid,omitempty"`
	FunFact       string    `json:"funFact,omitempty"`
	FavoriteFruit string    `json:"favoriteFruit,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}

type chatResponse struct {
	URI       string    `json:"uri"`
	AuthorDID string    `json:"authorDid"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"createdAt"`
}

type replyResponse struct {
	URI       string    `json:"uri"`
	AuthorDID string    `json:"authorDid"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"createdAt"`
}

type logResponse struct {
	URI       string    `json:"uri"`
	AuthorDID string    `json:"authorDid"`
	Fruit     string    `json:"fruit"`
	Count     int       `json:"count"`
	CreatedAt time.Time `json:"createdAt"`
}

func New(store *index.Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /getMembers", func(w http.ResponseWriter, r *http.Request) {
		members, err := store.GetMembers()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp := make([]memberResponse, len(members))
		for i, m := range members {
			resp[i] = memberResponse{URI: m.URI, DID: m.DID, DisplayName: m.DisplayName, AvatarCID: m.AvatarCID, FunFact: m.FunFact, FavoriteFruit: m.FavoriteFruit, CreatedAt: m.CreatedAt}
		}
		writeJSON(w, resp)
	})
	mux.HandleFunc("GET /getChats", func(w http.ResponseWriter, r *http.Request) {
		chats, err := store.GetChats()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp := make([]chatResponse, len(chats))
		for i, c := range chats {
			resp[i] = chatResponse{URI: c.URI, AuthorDID: c.AuthorDID, Text: c.Text, CreatedAt: c.CreatedAt}
		}
		writeJSON(w, resp)
	})
	mux.HandleFunc("GET /getReplies", func(w http.ResponseWriter, r *http.Request) {
		chatURI := r.URL.Query().Get("chatUri")
		if chatURI == "" {
			http.Error(w, "chatUri query param required", http.StatusBadRequest)
			return
		}
		replies, err := store.GetReplies(chatURI)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp := make([]replyResponse, len(replies))
		for i, rr := range replies {
			resp[i] = replyResponse{URI: rr.URI, AuthorDID: rr.AuthorDID, Text: rr.Text, CreatedAt: rr.CreatedAt}
		}
		writeJSON(w, resp)
	})
	mux.HandleFunc("GET /getLogs", func(w http.ResponseWriter, r *http.Request) {
		logs, err := store.GetLogs()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp := make([]logResponse, len(logs))
		for i, l := range logs {
			resp[i] = logResponse{URI: l.URI, AuthorDID: l.AuthorDID, Fruit: l.Fruit, Count: l.Count, CreatedAt: l.CreatedAt}
		}
		writeJSON(w, resp)
	})
	return corsMiddleware(mux)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
cd demos/fruitgang/backend && go test ./internal/server/...
```

Expected: PASS — all 5 tests green.

- [ ] **Step 5: Commit**

```bash
git add demos/fruitgang/backend/internal/server/
git commit -m "feat(fruitgang): backend HTTP server with read endpoints"
```

---

### Task 5: Backend cmd/main.go + moon.yml

**Files:**
- Create: `demos/fruitgang/backend/cmd/main.go`
- Create: `demos/fruitgang/backend/moon.yml`

**Interfaces:**
- Consumes: `index.NewStore`, `indexer.Run`, `server.New`, `sap.NewSap`, `oauthclient.NewGormStore`, `oauthclient.NewApp`
- Produces: runnable binary at `demos/fruitgang/backend/cmd/main.go`; `moon r fruitgang-backend:dev` starts it

- [ ] **Step 1: Write main.go**

```go
// demos/fruitgang/backend/cmd/main.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/habitat-network/habitat/demos/fruitgang/internal/index"
	"github.com/habitat-network/habitat/demos/fruitgang/internal/indexer"
	"github.com/habitat-network/habitat/demos/fruitgang/internal/server"
	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/habitat-network/habitat/internal/sap"
	"github.com/urfave/cli/v3"
	"golang.org/x/sync/errgroup"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	if err := run(os.Args); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	app := &cli.Command{
		Name:   "fruitgang-backend",
		Usage:  "Fruit Gang demo backend",
		Flags:  flags(),
		Action: start,
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	return app.Run(ctx, args)
}

var (
	fDB     = "db"
	fPort   = "port"
	fDomain = "domain"
	fSecret = "secret"
)

func flags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: fDB, Value: "sqlite://fruitgang.db", Sources: cli.EnvVars("FG_DB")},
		&cli.StringFlag{Name: fPort, Value: "3100", Sources: cli.EnvVars("FG_PORT")},
		&cli.StringFlag{Name: fDomain, Value: "fruitgang-api.local.habitat.network", Sources: cli.EnvVars("FG_DOMAIN")},
		&cli.StringFlag{Name: fSecret, Value: "", Sources: cli.EnvVars("FG_SECRET")},
	}
}

func start(ctx context.Context, cmd *cli.Command) error {
	db, err := openDB(cmd.String(fDB))
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}

	store, err := index.NewStore(db)
	if err != nil {
		return fmt.Errorf("init index store: %w", err)
	}

	secretStr := cmd.String(fSecret)
	oauthStore, err := oauthclient.NewGormStore(db)
	if err != nil {
		return fmt.Errorf("create oauth store: %w", err)
	}

	domain := cmd.String(fDomain)
	oauthCfg := oauth.NewPublicConfig(
		"https://"+domain+"/client-metadata.json",
		"https://"+domain+"/oauth-callback",
		[]string{"atproto"},
	)

	if secretStr != "" {
		secret, err := atcrypto.ParsePrivateMultibase(secretStr)
		if err != nil {
			return fmt.Errorf("parse secret: %w", err)
		}
		if err := oauthCfg.SetClientSecret(secret, "fruitgang"); err != nil {
			return fmt.Errorf("set client secret: %w", err)
		}
	}

	oauthApp := oauthclient.NewApp(&oauthCfg, oauthStore)

	s, err := sap.NewSap(sap.SapConfig{DB: db, OAuthClient: oauthApp})
	if err != nil {
		return fmt.Errorf("create sap: %w", err)
	}

	mux := server.New(store)

	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error { return s.Start(ctx) })
	eg.Go(func() error { return indexer.Run(ctx, s.Outbox, store) })
	eg.Go(func() error {
		srv := &http.Server{Addr: ":" + cmd.String(fPort), Handler: mux}
		go func() { _ = srv.ListenAndServe() }()
		<-ctx.Done()
		return srv.Shutdown(context.Background())
	})
	return eg.Wait()
}

func openDB(dsn string) (*gorm.DB, error) {
	if !strings.HasPrefix(dsn, "sqlite://") {
		return nil, fmt.Errorf("only sqlite:// DSNs are supported, got: %s", dsn)
	}
	path := strings.TrimPrefix(dsn, "sqlite://")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Warn)})
	if err != nil {
		return nil, err
	}
	db.Exec("PRAGMA journal_mode=WAL;")
	db.Exec("PRAGMA synchronous=NORMAL;")
	db.Exec("PRAGMA busy_timeout=10000;")
	return db, nil
}
```

- [ ] **Step 2: Verify it compiles**

```bash
cd demos/fruitgang/backend && go build ./cmd
```

Expected: binary produced (or "." if no output flag), exit 0.

- [ ] **Step 3: Write moon.yml**

```yaml
# demos/fruitgang/backend/moon.yml
layer: application
tasks:
  dev:
    command: go
    args:
      - run
      - ./cmd
    deps:
      - pear:dev
    env:
      FG_PORT: "3100"
      FG_DOMAIN: "fruitgang-api.local.habitat.network"
    options:
      envFile:
        - /dev.env
    preset: server
  build:
    command: go
    args:
      - build
      - -o
      - $workspaceRoot/bin/fruitgang-backend
      - ./cmd
    inputs:
      - "**/*.go"
      - go.mod
      - go.sum
    outputs:
      - /bin/fruitgang-backend
```

- [ ] **Step 4: Commit**

```bash
git add demos/fruitgang/backend/cmd/ demos/fruitgang/backend/moon.yml
git commit -m "feat(fruitgang): backend entrypoint + moon dev task"
```

---

### Task 6: Frontend scaffold — package.json, tsconfig, vite, theme, fruits map

**Files:**
- Create: `demos/fruitgang/frontend/package.json`
- Create: `demos/fruitgang/frontend/tsconfig.json`
- Create: `demos/fruitgang/frontend/vite.config.ts`
- Create: `demos/fruitgang/frontend/index.html`
- Create: `demos/fruitgang/frontend/moon.yml`
- Create: `demos/fruitgang/frontend/src/globals.d.ts`
- Create: `demos/fruitgang/frontend/src/theme.css`
- Create: `demos/fruitgang/frontend/src/fruits.ts`

**Interfaces:**
- Produces: `FRUITS` map exported from `src/fruits.ts` — keyed by full lexicon slug (e.g. `"community.fruitgang.member#strawberry"`), value `{ emoji: string; label: string; colorVar: string }` where `colorVar` is a CSS custom property name (e.g. `"--strawberry"`)
- Produces: `__FRUITGANG_API__` injected by vite as the backend base URL; `__DOMAIN__` and `__HABITAT_DOMAIN__` injected by `habitatPlugins`

- [ ] **Step 1: Write package.json**

```json
// demos/fruitgang/frontend/package.json
{
  "name": "fruitgang-frontend",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite dev",
    "build": "tsc --build --force && vite build"
  },
  "dependencies": {
    "@tanstack/react-devtools": "catalog:",
    "@tanstack/react-query": "catalog:",
    "@tanstack/react-router": "catalog:",
    "@tanstack/react-router-devtools": "catalog:",
    "@tanstack/router-plugin": "catalog:",
    "internal": "workspace:*",
    "react": "catalog:",
    "react-dom": "catalog:",
    "vite-tsconfig-paths": "catalog:"
  },
  "devDependencies": {
    "@tanstack/devtools-vite": "catalog:",
    "@types/node": "catalog:",
    "@types/react": "catalog:",
    "@types/react-dom": "catalog:",
    "@vitejs/plugin-react": "catalog:",
    "typescript": "catalog:",
    "vite": "catalog:"
  }
}
```

- [ ] **Step 2: Write tsconfig.json**

```json
// demos/fruitgang/frontend/tsconfig.json
{
  "extends": "../../../tsconfig.options.json",
  "compilerOptions": {
    "baseUrl": ".",
    "noEmit": true,
    "paths": {
      "@/*": ["./src/*"]
    },
    "types": ["vite/client"]
  },
  "include": ["src/**/*"],
  "references": [
    { "path": "../../../typescript/internal" }
  ]
}
```

- [ ] **Step 3: Write vite.config.ts**

```ts
// demos/fruitgang/frontend/vite.config.ts
import { defineConfig } from "vite";
import viteReact from "@vitejs/plugin-react";
import viteTsConfigPaths from "vite-tsconfig-paths";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import habitatPlugins from "internal/habitatAppVitePlugin";

export default defineConfig({
  plugins: [
    viteTsConfigPaths({ projects: ["./tsconfig.json"] }),
    tanstackRouter(),
    viteReact(),
    ...habitatPlugins({ name: "Fruit Gang" }),
    {
      name: "fruitgang-api-config",
      config() {
        return {
          define: {
            __FRUITGANG_API__: JSON.stringify(
              process.env.FRUITGANG_API ?? "https://fruitgang-api.local.habitat.network"
            ),
          },
        };
      },
    },
  ],
});
```

- [ ] **Step 4: Write index.html**

```html
<!-- demos/fruitgang/frontend/index.html -->
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <link rel="stylesheet" href="/src/theme.css" />
    <title>fruit gang 🍓</title>
  </head>
  <body>
    <main id="app"></main>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

- [ ] **Step 5: Write moon.yml**

```yaml
# demos/fruitgang/frontend/moon.yml
layer: application
tasks:
  dev:
    env:
      SERVER_PORT: "5178"
      DOMAIN: "fruitgang.local.habitat.network"
      HABITAT_DOMAIN: "pear.local.habitat.network"
      FRUITGANG_API: "https://fruitgang-api.local.habitat.network"
    deps:
      - fruitgang-backend:dev
      - root:caddy
```

- [ ] **Step 6: Write globals.d.ts**

```ts
// demos/fruitgang/frontend/src/globals.d.ts
declare const __DOMAIN__: string;
declare const __HABITAT_DOMAIN__: string;
declare const __HASH_ROUTING__: boolean;
declare const __FRUITGANG_API__: string;
```

- [ ] **Step 7: Write theme.css — the single source of truth for all design tokens**

```css
/* demos/fruitgang/frontend/src/theme.css */

@import url('https://fonts.googleapis.com/css2?family=Original+Surfer&family=Quicksand:wght@400;500;600;700&display=swap');
@import "tailwindcss";

@theme {
  /* Fruit palette */
  --color-strawberry: #ff2d6f;
  --color-tangerine: #fc6b04;
  --color-banana: #ffef0a;
  --color-lime: #8ee71b;
  --color-grape: #a256c8;
  --color-blueberry: #3224b1;
  --color-cherry: #f11616;
  --color-peach: #ffc062;

  /* Surfaces */
  --color-bg: #09090f;
  --color-surface: #13131f;
  --color-surface-raised: #1c1c2e;
  --color-border: #2a2a40;

  /* Text */
  --color-text: #f5f5f7;
  --color-muted: #9595b8;

  /* Typography */
  --font-display: 'Original Surfer', cursive;
  --font-body: 'Quicksand', sans-serif;

  /* Radii */
  --radius-card: 20px;
  --radius-pill: 9999px;
  --radius-input: 12px;
}

/* CSS custom properties (accessible as var(--strawberry) in inline styles) */
:root {
  --strawberry: #ff2d6f;
  --tangerine: #fc6b04;
  --banana: #ffef0a;
  --lime: #8ee71b;
  --grape: #a256c8;
  --blueberry: #3224b1;
  --cherry: #f11616;
  --peach: #ffc062;

  --bg: #09090f;
  --surface: #13131f;
  --surface-raised: #1c1c2e;
  --border: #2a2a40;

  --text: #f5f5f7;
  --muted: #9595b8;

  --font-display: 'Original Surfer', cursive;
  --font-body: 'Quicksand', sans-serif;

  --radius-card: 20px;
  --radius-pill: 9999px;
  --radius-input: 12px;
}

html, body {
  background-color: var(--bg);
  color: var(--text);
  font-family: var(--font-body);
  margin: 0;
  padding: 0;
}

* {
  box-sizing: border-box;
}
```

- [ ] **Step 8: Write fruits.ts — single source of fruit metadata**

The slugs must match exactly what the lexicons define (Task 1 Step 6–9).
Use `"community.fruitgang.member#X"` for the member lexicon — both `member` and `log` slugs share the same base names, so we key by just the suffix (e.g. `"strawberry"`) and the caller prepends the namespace. Actually, to keep it simple: key by the suffix slug only; callers strip the namespace prefix.

```ts
// demos/fruitgang/frontend/src/fruits.ts

export interface FruitMeta {
  emoji: string;
  label: string;
  colorVar: string; // CSS custom property name, e.g. "--strawberry"
}

// Keyed by the token suffix (the part after "#"), e.g. "strawberry"
// The lexicon stores full slugs like "community.fruitgang.log#strawberry".
// Use fruitSlug(fullSlug) to get the key.
export const FRUITS: Record<string, FruitMeta> = {
  greenApple:  { emoji: "🍏", label: "Green Apple",  colorVar: "--lime" },
  redApple:    { emoji: "🍎", label: "Red Apple",    colorVar: "--cherry" },
  pear:        { emoji: "🍐", label: "Pear",         colorVar: "--lime" },
  tangerine:   { emoji: "🍊", label: "Tangerine",    colorVar: "--tangerine" },
  lemon:       { emoji: "🍋", label: "Lemon",        colorVar: "--banana" },
  lime:        { emoji: "🍋‍🟩", label: "Lime",        colorVar: "--lime" },
  banana:      { emoji: "🍌", label: "Banana",       colorVar: "--banana" },
  watermelon:  { emoji: "🍉", label: "Watermelon",   colorVar: "--strawberry" },
  grapes:      { emoji: "🍇", label: "Grapes",       colorVar: "--grape" },
  strawberry:  { emoji: "🍓", label: "Strawberry",   colorVar: "--strawberry" },
  blueberry:   { emoji: "🫐", label: "Blueberry",    colorVar: "--blueberry" },
  melon:       { emoji: "🍈", label: "Melon",        colorVar: "--lime" },
  cherries:    { emoji: "🍒", label: "Cherries",     colorVar: "--cherry" },
  peach:       { emoji: "🍑", label: "Peach",        colorVar: "--peach" },
  mango:       { emoji: "🥭", label: "Mango",        colorVar: "--tangerine" },
  pineapple:   { emoji: "🍍", label: "Pineapple",    colorVar: "--banana" },
  coconut:     { emoji: "🥥", label: "Coconut",      colorVar: "--muted" },
  kiwi:        { emoji: "🥝", label: "Kiwi",         colorVar: "--lime" },
  tomato:      { emoji: "🍅", label: "Tomato",       colorVar: "--cherry" },
  olive:       { emoji: "🫒", label: "Olive",        colorVar: "--lime" },
  avocado:     { emoji: "🥑", label: "Avocado",      colorVar: "--lime" },
};

// Strips the namespace prefix from a full lexicon token slug.
// "community.fruitgang.log#strawberry" → "strawberry"
export function fruitSlug(fullSlug: string): string {
  const idx = fullSlug.indexOf("#");
  return idx >= 0 ? fullSlug.slice(idx + 1) : fullSlug;
}

// Returns fruit metadata for a full lexicon token slug, or undefined if unknown.
export function getFruit(fullSlug: string): FruitMeta | undefined {
  return FRUITS[fruitSlug(fullSlug)];
}

export const FRUIT_KEYS = Object.keys(FRUITS);
```

- [ ] **Step 9: Install dependencies**

```bash
cd /path/to/repo && pnpm install
```

Expected: fruitgang-frontend deps resolved; no errors.

- [ ] **Step 10: Verify build (type-check only so far)**

```bash
cd demos/fruitgang/frontend && pnpm build
```

Expected: may fail on missing routes — that's fine at this step. Should not fail on tsconfig or vite config errors. If it fails on `"internal/habitatAppVitePlugin"`, confirm the tsconfig references and pnpm workspace are correctly set up.

- [ ] **Step 11: Commit**

```bash
git add demos/fruitgang/frontend/
git commit -m "feat(fruitgang): frontend scaffold — theme, fruits map, vite config"
```

---

### Task 7: Frontend routing + auth shell

**Files:**
- Create: `demos/fruitgang/frontend/src/main.tsx`
- Create: `demos/fruitgang/frontend/src/routes/__root.tsx`
- Create: `demos/fruitgang/frontend/src/routes/login.tsx`
- Create: `demos/fruitgang/frontend/src/routes/_app.tsx`

**Interfaces:**
- Consumes: `AuthManager` from `internal`; `__DOMAIN__`, `__HABITAT_DOMAIN__`, `__FRUITGANG_API__` globals
- Produces: router context `{ queryClient: QueryClient; authManager: AuthManager }` available to all routes; nav layout with tab links to `/members`, `/chats`, `/log`; active tab glow derived from logged-in user's `favoriteFruit`

- [ ] **Step 1: Write main.tsx**

```tsx
// demos/fruitgang/frontend/src/main.tsx
import { StrictMode } from "react";
import ReactDOM from "react-dom/client";
import { RouterProvider, createRouter } from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthManager } from "internal";
import { routeTree } from "./routeTree.gen";
import "./theme.css";

const authManager = new AuthManager(
  "Fruit Gang",
  __DOMAIN__,
  __HABITAT_DOMAIN__,
  () => { router.navigate({ to: "/login" }); }
);

const queryClient = new QueryClient({
  defaultOptions: { queries: { staleTime: 1000 * 30 } },
});

const router = createRouter({
  routeTree,
  context: { queryClient, authManager },
  defaultPreload: "intent",
  defaultPreloadStaleTime: 0,
});

declare module "@tanstack/react-router" {
  interface Register { router: typeof router; }
}

const root = document.getElementById("app");
if (root && !root.innerHTML) {
  ReactDOM.createRoot(root).render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </StrictMode>
  );
}
```

- [ ] **Step 2: Write __root.tsx**

```tsx
// demos/fruitgang/frontend/src/routes/__root.tsx
import type { AuthManager } from "internal";
import type { QueryClient } from "@tanstack/react-query";
import { Outlet, createRootRouteWithContext } from "@tanstack/react-router";

interface RouterContext {
  queryClient: QueryClient;
  authManager: AuthManager;
}

export const Route = createRootRouteWithContext<RouterContext>()({
  component() {
    return <Outlet />;
  },
});
```

- [ ] **Step 3: Write login.tsx**

```tsx
// demos/fruitgang/frontend/src/routes/login.tsx
import { createFileRoute } from "@tanstack/react-router";
import { AuthForm } from "internal";

export const Route = createFileRoute("/login")({
  component() {
    const { authManager } = Route.useRouteContext();
    return (
      <div style={{
        minHeight: "100vh",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        background: "var(--bg)",
        gap: "2rem",
      }}>
        <h1 style={{
          fontFamily: "var(--font-display)",
          fontSize: "3rem",
          color: "var(--text)",
          margin: 0,
        }}>
          🍓 fruit gang
        </h1>
        <AuthForm authManager={authManager} redirectUrl={`https://${__DOMAIN__}`} />
      </div>
    );
  },
});
```

- [ ] **Step 4: Write _app.tsx (auth guard + nav layout)**

This is the most complex route. It:
1. Guards with auth (same `beforeLoad` pattern as the docs app)
2. Loads the current user's member profile from the backend to get their `favoriteFruit`
3. Renders a persistent top nav with a fruit-color glow on the active tab

```tsx
// demos/fruitgang/frontend/src/routes/_app.tsx
import { createFileRoute, Link, Outlet, redirect } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { getFruit } from "@/fruits";

async function fetchMembers(): Promise<Array<{ did: string; favoriteFruit?: string }>> {
  const res = await fetch(`${__FRUITGANG_API__}/getMembers`);
  if (!res.ok) return [];
  return res.json();
}

export const Route = createFileRoute("/_app")({
  async beforeLoad({ context }) {
    await context.authManager.maybeExchangeCode();
    if (!context.authManager.getAuthInfo()) {
      throw redirect({ to: "/login" });
    }
  },
  component: AppLayout,
});

function AppLayout() {
  const { authManager } = Route.useRouteContext();
  const did = authManager.getAuthInfo()?.did ?? "";

  const { data: members = [] } = useQuery({
    queryKey: ["members"],
    queryFn: fetchMembers,
    staleTime: 1000 * 30,
  });

  const myFruit = members.find((m) => m.did === did)?.favoriteFruit;
  const accentColor = myFruit
    ? `var(${getFruit(myFruit)?.colorVar ?? "--strawberry"})`
    : "var(--strawberry)";

  return (
    <div style={{ minHeight: "100vh", background: "var(--bg)", fontFamily: "var(--font-body)" }}>
      <nav style={{
        position: "sticky", top: 0, zIndex: 10,
        background: "var(--surface)",
        borderBottom: "1px solid var(--border)",
        display: "flex", alignItems: "center",
        padding: "0 1.5rem", height: "60px", gap: "1rem",
      }}>
        <span style={{
          fontFamily: "var(--font-display)",
          fontSize: "1.4rem",
          color: "var(--text)",
          marginRight: "auto",
          userSelect: "none",
        }}>
          🍓 fruit gang
        </span>

        <NavTab to="/members" label="Members" accentColor={accentColor} />
        <NavTab to="/chats" label="Chats" accentColor={accentColor} />
        <NavTab to="/log" label="Log" accentColor={accentColor} />

        <button
          onClick={() => authManager.logout()}
          style={{
            marginLeft: "auto",
            background: "none",
            border: "1px solid var(--border)",
            color: "var(--muted)",
            borderRadius: "var(--radius-pill)",
            padding: "0.3rem 0.9rem",
            cursor: "pointer",
            fontFamily: "var(--font-body)",
            fontSize: "0.8rem",
          }}
        >
          sign out
        </button>
      </nav>

      <main style={{ maxWidth: "860px", margin: "0 auto", padding: "2rem 1.5rem" }}>
        <Outlet />
      </main>
    </div>
  );
}

function NavTab({ to, label, accentColor }: { to: string; label: string; accentColor: string }) {
  return (
    <Link
      to={to}
      style={{ textDecoration: "none" }}
    >
      {({ isActive }) => (
        <span style={{
          display: "inline-block",
          padding: "0.35rem 1rem",
          borderRadius: "var(--radius-pill)",
          fontFamily: "var(--font-body)",
          fontWeight: 600,
          fontSize: "0.9rem",
          color: isActive ? "var(--text)" : "var(--muted)",
          background: isActive ? "var(--surface-raised)" : "transparent",
          boxShadow: isActive ? `0 0 12px 2px ${accentColor}55` : "none",
          border: isActive ? `1px solid ${accentColor}88` : "1px solid transparent",
          transition: "all 0.2s ease",
        }}>
          {label}
        </span>
      )}
    </Link>
  );
}
```

- [ ] **Step 5: Create index redirect route**

```tsx
// demos/fruitgang/frontend/src/routes/index.tsx
import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/")({
  beforeLoad() {
    throw redirect({ to: "/members" });
  },
});
```

- [ ] **Step 6: Verify build passes**

```bash
cd demos/fruitgang/frontend && pnpm build
```

Expected: PASS — TanStack Router generates `routeTree.gen.ts` and TypeScript is satisfied. If you see "Cannot find module 'internal/habitatAppVitePlugin'", run `pnpm install` from the repo root first.

- [ ] **Step 7: Commit**

```bash
git add demos/fruitgang/frontend/src/
git commit -m "feat(fruitgang): frontend routing + auth shell + nav layout"
```

---

### Task 8: Members page

**Files:**
- Create: `demos/fruitgang/frontend/src/routes/_app/members.tsx`

**Interfaces:**
- Consumes: `GET /getMembers` → `Array<{ uri, did, displayName, avatarCid, funFact, favoriteFruit, createdAt }>`; `procedure("network.habitat.repo.putRecord", ...)` from `internal` for profile creation
- Produces: `/members` route rendered in `_app` layout

- [ ] **Step 1: Write members.tsx**

```tsx
// demos/fruitgang/frontend/src/routes/_app/members.tsx
import { createFileRoute } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { procedure } from "internal";
import { useState } from "react";
import { FRUITS, FRUIT_KEYS, getFruit } from "@/fruits";

interface MemberRecord {
  uri: string;
  did: string;
  displayName: string;
  avatarCid?: string;
  funFact?: string;
  favoriteFruit?: string;
  createdAt: string;
}

async function fetchMembers(): Promise<MemberRecord[]> {
  const res = await fetch(`${__FRUITGANG_API__}/getMembers`);
  if (!res.ok) throw new Error("failed to fetch members");
  return res.json();
}

export const Route = createFileRoute("/_app/members")({
  component: MembersPage,
});

function MembersPage() {
  const { authManager } = Route.useRouteContext();
  const qc = useQueryClient();
  const did = authManager.getAuthInfo()?.did ?? "";
  const { data: members = [], isLoading } = useQuery({ queryKey: ["members"], queryFn: fetchMembers });

  const hasMember = members.some((m) => m.did === did);
  const [form, setForm] = useState({ displayName: "", funFact: "", favoriteFruit: "strawberry" });

  const { mutate: createProfile, isPending } = useMutation({
    mutationFn: async () => {
      await procedure("network.habitat.repo.putRecord", {
        repo: did,
        collection: "community.fruitgang.member",
        record: {
          displayName: form.displayName,
          funFact: form.funFact,
          favoriteFruit: `community.fruitgang.member#${form.favoriteFruit}`,
          createdAt: new Date().toISOString(),
        },
      }, { authManager });
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["members"] }),
  });

  return (
    <div>
      <h2 style={{ fontFamily: "var(--font-display)", fontSize: "2rem", color: "var(--text)", marginBottom: "1.5rem" }}>
        the gang 🍉
      </h2>

      {!hasMember && (
        <div style={{
          background: "var(--surface)",
          border: "1px dashed var(--border)",
          borderRadius: "var(--radius-card)",
          padding: "1.5rem",
          marginBottom: "2rem",
        }}>
          <p style={{ color: "var(--muted)", fontWeight: 600, marginBottom: "1rem" }}>
            you're not in the gang yet — introduce yourself!
          </p>
          <div style={{ display: "flex", flexDirection: "column", gap: "0.75rem", maxWidth: "400px" }}>
            <input
              placeholder="display name"
              value={form.displayName}
              onChange={(e) => setForm((f) => ({ ...f, displayName: e.target.value }))}
              style={inputStyle}
            />
            <input
              placeholder="fun fact"
              value={form.funFact}
              onChange={(e) => setForm((f) => ({ ...f, funFact: e.target.value }))}
              style={inputStyle}
            />
            <select
              value={form.favoriteFruit}
              onChange={(e) => setForm((f) => ({ ...f, favoriteFruit: e.target.value }))}
              style={inputStyle}
            >
              {FRUIT_KEYS.map((key) => (
                <option key={key} value={key}>
                  {FRUITS[key].emoji} {FRUITS[key].label}
                </option>
              ))}
            </select>
            <button
              onClick={() => createProfile()}
              disabled={isPending || !form.displayName}
              style={buttonStyle("var(--strawberry)")}
            >
              {isPending ? "joining…" : "join the gang 🍓"}
            </button>
          </div>
        </div>
      )}

      {isLoading ? (
        <p style={{ color: "var(--muted)" }}>loading members…</p>
      ) : (
        <div style={{
          display: "grid",
          gridTemplateColumns: "repeat(auto-fill, minmax(220px, 1fr))",
          gap: "1rem",
        }}>
          {members.map((m) => <MemberCard key={m.uri} member={m} />)}
        </div>
      )}
    </div>
  );
}

function MemberCard({ member }: { member: MemberRecord }) {
  const fruit = member.favoriteFruit ? getFruit(member.favoriteFruit) : undefined;
  const accentColor = fruit ? `var(${fruit.colorVar})` : "var(--muted)";
  const joinDate = new Date(member.createdAt).toLocaleDateString("en-US", { month: "short", year: "numeric" });

  return (
    <div style={{
      background: "var(--surface)",
      border: `1px solid var(--border)`,
      borderRadius: "var(--radius-card)",
      padding: "1.25rem",
      display: "flex",
      flexDirection: "column",
      gap: "0.5rem",
      boxShadow: `0 0 0 1px ${accentColor}22`,
    }}>
      <div style={{ display: "flex", alignItems: "center", gap: "0.75rem" }}>
        <div style={{
          width: 48, height: 48, borderRadius: "50%",
          background: "var(--surface-raised)",
          border: `2px solid ${accentColor}`,
          boxShadow: `0 0 10px ${accentColor}66`,
          display: "flex", alignItems: "center", justifyContent: "center",
          fontSize: "1.5rem",
          flexShrink: 0,
        }}>
          {fruit?.emoji ?? "🍑"}
        </div>
        <div>
          <div style={{ fontWeight: 700, color: "var(--text)", fontSize: "1rem" }}>
            {member.displayName}
          </div>
          {fruit && (
            <div style={{ fontSize: "0.75rem", color: accentColor, fontWeight: 600 }}>
              {fruit.emoji} {fruit.label} fan
            </div>
          )}
        </div>
      </div>
      {member.funFact && (
        <p style={{ margin: 0, fontSize: "0.85rem", color: "var(--muted)", fontStyle: "italic" }}>
          "{member.funFact}"
        </p>
      )}
      <p style={{ margin: 0, fontSize: "0.75rem", color: "var(--border)", marginTop: "auto" }}>
        since {joinDate}
      </p>
    </div>
  );
}

const inputStyle: React.CSSProperties = {
  background: "var(--surface-raised)",
  border: "1px solid var(--border)",
  borderRadius: "var(--radius-input)",
  color: "var(--text)",
  padding: "0.6rem 0.9rem",
  fontFamily: "var(--font-body)",
  fontSize: "0.9rem",
  outline: "none",
  width: "100%",
};

function buttonStyle(color: string): React.CSSProperties {
  return {
    background: color,
    border: "none",
    borderRadius: "var(--radius-pill)",
    color: "#000",
    fontFamily: "var(--font-display)",
    fontSize: "1rem",
    padding: "0.6rem 1.25rem",
    cursor: "pointer",
    fontWeight: 700,
  };
}
```

- [ ] **Step 2: Verify build passes**

```bash
cd demos/fruitgang/frontend && pnpm build
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add demos/fruitgang/frontend/src/routes/_app/members.tsx
git commit -m "feat(fruitgang): members page with profile creation"
```

---

### Task 9: Chats page

**Files:**
- Create: `demos/fruitgang/frontend/src/routes/_app/chats.tsx`

**Interfaces:**
- Consumes: `GET /getChats` → `Array<{ uri, authorDid, text, createdAt }>`; `GET /getReplies?chatUri=` → same shape; `procedure("network.habitat.repo.putRecord", ...)` for posting

- [ ] **Step 1: Write chats.tsx**

```tsx
// demos/fruitgang/frontend/src/routes/_app/chats.tsx
import { createFileRoute } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { procedure } from "internal";
import { useState } from "react";
import { getFruit } from "@/fruits";

interface ChatRecord { uri: string; authorDid: string; text: string; createdAt: string; }
interface MemberRecord { did: string; displayName?: string; favoriteFruit?: string; }

const fetchChats = async (): Promise<ChatRecord[]> => {
  const res = await fetch(`${__FRUITGANG_API__}/getChats`);
  if (!res.ok) throw new Error("fetch chats failed");
  return res.json();
};

const fetchReplies = async (chatUri: string): Promise<ChatRecord[]> => {
  const res = await fetch(`${__FRUITGANG_API__}/getReplies?chatUri=${encodeURIComponent(chatUri)}`);
  if (!res.ok) return [];
  return res.json();
};

const fetchMembers = async (): Promise<MemberRecord[]> => {
  const res = await fetch(`${__FRUITGANG_API__}/getMembers`);
  if (!res.ok) return [];
  return res.json();
};

export const Route = createFileRoute("/_app/chats")({
  component: ChatsPage,
});

function ChatsPage() {
  const { authManager } = Route.useRouteContext();
  const qc = useQueryClient();
  const did = authManager.getAuthInfo()?.did ?? "";
  const [newText, setNewText] = useState("");
  const [expandedUri, setExpandedUri] = useState<string | null>(null);

  const { data: chats = [], isLoading } = useQuery({ queryKey: ["chats"], queryFn: fetchChats });
  const { data: members = [] } = useQuery({ queryKey: ["members"], queryFn: fetchMembers });
  const memberMap = Object.fromEntries(members.map((m) => [m.did, m]));

  const { mutate: postChat, isPending: posting } = useMutation({
    mutationFn: async (text: string) => {
      await procedure("network.habitat.repo.putRecord", {
        repo: did,
        collection: "community.fruitgang.chat",
        record: { text, createdAt: new Date().toISOString() },
      }, { authManager });
    },
    onSuccess: () => { setNewText(""); qc.invalidateQueries({ queryKey: ["chats"] }); },
  });

  return (
    <div>
      <h2 style={{ fontFamily: "var(--font-display)", fontSize: "2rem", color: "var(--text)", marginBottom: "1.5rem" }}>
        fruit chats 🍒
      </h2>

      <div style={{
        background: "var(--surface)",
        border: "1px solid var(--border)",
        borderRadius: "var(--radius-card)",
        padding: "1rem",
        marginBottom: "1.5rem",
        display: "flex",
        gap: "0.75rem",
        alignItems: "flex-end",
      }}>
        <textarea
          placeholder="say something fruity…"
          value={newText}
          maxLength={300}
          onChange={(e) => setNewText(e.target.value)}
          rows={2}
          style={{
            flex: 1, resize: "none",
            background: "var(--surface-raised)",
            border: "1px solid var(--border)",
            borderRadius: "var(--radius-input)",
            color: "var(--text)",
            padding: "0.6rem 0.9rem",
            fontFamily: "var(--font-body)",
            fontSize: "0.9rem",
            outline: "none",
          }}
        />
        <button
          onClick={() => newText.trim() && postChat(newText.trim())}
          disabled={posting || !newText.trim()}
          style={{
            background: "var(--tangerine)",
            border: "none",
            borderRadius: "var(--radius-pill)",
            color: "#000",
            fontFamily: "var(--font-display)",
            fontSize: "1rem",
            padding: "0.6rem 1.25rem",
            cursor: "pointer",
            whiteSpace: "nowrap",
          }}
        >
          {posting ? "posting…" : "post 🍊"}
        </button>
      </div>

      {isLoading ? (
        <p style={{ color: "var(--muted)" }}>loading chats…</p>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: "0.75rem" }}>
          {chats.map((chat) => (
            <ChatItem
              key={chat.uri}
              chat={chat}
              member={memberMap[chat.authorDid]}
              isExpanded={expandedUri === chat.uri}
              onExpand={() => setExpandedUri((u) => (u === chat.uri ? null : chat.uri))}
              authManager={authManager}
              currentDid={did}
              onReplyPosted={() => qc.invalidateQueries({ queryKey: ["replies", chat.uri] })}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function ChatItem({
  chat, member, isExpanded, onExpand, authManager, currentDid, onReplyPosted,
}: {
  chat: ChatRecord;
  member?: MemberRecord;
  isExpanded: boolean;
  onExpand: () => void;
  authManager: any;
  currentDid: string;
  onReplyPosted: () => void;
}) {
  const fruit = member?.favoriteFruit ? getFruit(member.favoriteFruit) : undefined;
  const accentColor = fruit ? `var(${fruit.colorVar})` : "var(--muted)";
  const [replyText, setReplyText] = useState("");
  const qc = useQueryClient();

  const { data: replies = [] } = useQuery({
    queryKey: ["replies", chat.uri],
    queryFn: () => fetchReplies(chat.uri),
    enabled: isExpanded,
  });

  const { mutate: postReply, isPending: replying } = useMutation({
    mutationFn: async (text: string) => {
      await procedure("network.habitat.repo.putRecord", {
        repo: currentDid,
        collection: "community.fruitgang.chatReply",
        record: { text, replyTo: chat.uri, createdAt: new Date().toISOString() },
      }, { authManager });
    },
    onSuccess: () => { setReplyText(""); qc.invalidateQueries({ queryKey: ["replies", chat.uri] }); onReplyPosted(); },
  });

  return (
    <div style={{
      background: "var(--surface)",
      border: "1px solid var(--border)",
      borderLeft: `3px solid ${accentColor}`,
      borderRadius: "var(--radius-card)",
      padding: "1rem",
    }}>
      <div style={{ display: "flex", justifyContent: "space-between", marginBottom: "0.4rem" }}>
        <span style={{ fontWeight: 700, color: "var(--text)", fontSize: "0.9rem" }}>
          {fruit?.emoji ?? "🍑"} {member?.displayName ?? chat.authorDid.slice(0, 12) + "…"}
        </span>
        <span style={{ fontSize: "0.75rem", color: "var(--muted)" }}>
          {new Date(chat.createdAt).toLocaleString()}
        </span>
      </div>
      <p style={{ margin: "0 0 0.5rem", color: "var(--text)", lineHeight: 1.5 }}>{chat.text}</p>
      <button
        onClick={onExpand}
        style={{
          background: "none", border: "none", color: "var(--muted)", fontSize: "0.8rem",
          cursor: "pointer", fontFamily: "var(--font-body)", padding: 0,
        }}
      >
        {isExpanded ? "▲ hide replies" : "▼ replies"}
      </button>

      {isExpanded && (
        <div style={{ marginTop: "0.75rem", paddingLeft: "1rem", borderLeft: "1px solid var(--border)", display: "flex", flexDirection: "column", gap: "0.5rem" }}>
          {replies.map((r) => (
            <div key={r.uri} style={{ fontSize: "0.9rem", color: "var(--muted)" }}>
              <strong style={{ color: "var(--text)" }}>{r.authorDid.slice(0, 12)}…</strong>{" "}
              {r.text}
            </div>
          ))}
          <div style={{ display: "flex", gap: "0.5rem", marginTop: "0.25rem" }}>
            <input
              placeholder="reply…"
              value={replyText}
              maxLength={300}
              onChange={(e) => setReplyText(e.target.value)}
              style={{
                flex: 1,
                background: "var(--surface-raised)",
                border: "1px solid var(--border)",
                borderRadius: "var(--radius-input)",
                color: "var(--text)",
                padding: "0.4rem 0.75rem",
                fontFamily: "var(--font-body)",
                fontSize: "0.85rem",
                outline: "none",
              }}
            />
            <button
              onClick={() => replyText.trim() && postReply(replyText.trim())}
              disabled={replying || !replyText.trim()}
              style={{
                background: accentColor, border: "none", borderRadius: "var(--radius-pill)",
                color: "#000", fontFamily: "var(--font-display)", fontSize: "0.85rem",
                padding: "0.4rem 1rem", cursor: "pointer",
              }}
            >
              {replying ? "…" : "reply"}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Verify build passes**

```bash
cd demos/fruitgang/frontend && pnpm build
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add demos/fruitgang/frontend/src/routes/_app/chats.tsx
git commit -m "feat(fruitgang): chats page with inline replies"
```

---

### Task 10: Fruit Log page

**Files:**
- Create: `demos/fruitgang/frontend/src/routes/_app/log.tsx`

**Interfaces:**
- Consumes: `GET /getLogs` → `Array<{ uri, authorDid, fruit, count, createdAt }>`; `procedure("network.habitat.repo.putRecord", ...)` for posting; `FRUITS`, `FRUIT_KEYS`, `getFruit` from `@/fruits`

- [ ] **Step 1: Write log.tsx**

```tsx
// demos/fruitgang/frontend/src/routes/_app/log.tsx
import { createFileRoute } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { procedure } from "internal";
import { useState } from "react";
import { FRUITS, FRUIT_KEYS, getFruit } from "@/fruits";

interface LogRecord { uri: string; authorDid: string; fruit: string; count: number; createdAt: string; }
interface MemberRecord { did: string; displayName?: string; favoriteFruit?: string; }

const fetchLogs = async (): Promise<LogRecord[]> => {
  const res = await fetch(`${__FRUITGANG_API__}/getLogs`);
  if (!res.ok) throw new Error("fetch logs failed");
  return res.json();
};

const fetchMembers = async (): Promise<MemberRecord[]> => {
  const res = await fetch(`${__FRUITGANG_API__}/getMembers`);
  if (!res.ok) return [];
  return res.json();
};

export const Route = createFileRoute("/_app/log")({
  component: LogPage,
});

function LogPage() {
  const { authManager } = Route.useRouteContext();
  const qc = useQueryClient();
  const did = authManager.getAuthInfo()?.did ?? "";

  const [selectedFruit, setSelectedFruit] = useState("strawberry");
  const [count, setCount] = useState(1);

  const { data: logs = [], isLoading } = useQuery({ queryKey: ["logs"], queryFn: fetchLogs });
  const { data: members = [] } = useQuery({ queryKey: ["members"], queryFn: fetchMembers });
  const memberMap = Object.fromEntries(members.map((m) => [m.did, m]));

  const { mutate: postLog, isPending: posting } = useMutation({
    mutationFn: async () => {
      await procedure("network.habitat.repo.putRecord", {
        repo: did,
        collection: "community.fruitgang.log",
        record: {
          fruit: `community.fruitgang.log#${selectedFruit}`,
          count,
          createdAt: new Date().toISOString(),
        },
      }, { authManager });
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["logs"] }),
  });

  const selectedMeta = FRUITS[selectedFruit];

  return (
    <div>
      <h2 style={{ fontFamily: "var(--font-display)", fontSize: "2rem", color: "var(--text)", marginBottom: "1.5rem" }}>
        fruit log 🍇
      </h2>

      <div style={{
        background: "var(--surface)",
        border: "1px solid var(--border)",
        borderRadius: "var(--radius-card)",
        padding: "1rem 1.25rem",
        marginBottom: "2rem",
        display: "flex",
        gap: "0.75rem",
        alignItems: "center",
        flexWrap: "wrap",
      }}>
        <select
          value={selectedFruit}
          onChange={(e) => setSelectedFruit(e.target.value)}
          style={{
            background: "var(--surface-raised)",
            border: "1px solid var(--border)",
            borderRadius: "var(--radius-input)",
            color: "var(--text)",
            padding: "0.55rem 0.9rem",
            fontFamily: "var(--font-body)",
            fontSize: "1rem",
            outline: "none",
            maxHeight: "200px",
            cursor: "pointer",
          }}
          size={1}
        >
          {FRUIT_KEYS.map((key) => (
            <option key={key} value={key}>
              {FRUITS[key].emoji} {FRUITS[key].label}
            </option>
          ))}
        </select>

        <input
          type="number"
          min={1}
          max={99}
          value={count}
          onChange={(e) => setCount(Math.min(99, Math.max(1, Number(e.target.value))))}
          style={{
            width: "5rem",
            background: "var(--surface-raised)",
            border: "1px solid var(--border)",
            borderRadius: "var(--radius-input)",
            color: "var(--text)",
            padding: "0.55rem 0.75rem",
            fontFamily: "var(--font-body)",
            fontSize: "1rem",
            outline: "none",
            textAlign: "center",
          }}
        />

        <div style={{ fontSize: "1.4rem", letterSpacing: "0.1em", flex: 1, minWidth: "80px" }}>
          {selectedMeta?.emoji.repeat(Math.min(count, 10))}{count > 10 ? `…×${count}` : ""}
        </div>

        <button
          onClick={() => postLog()}
          disabled={posting}
          style={{
            background: selectedMeta ? `var(${selectedMeta.colorVar})` : "var(--strawberry)",
            border: "none",
            borderRadius: "var(--radius-pill)",
            color: "#000",
            fontFamily: "var(--font-display)",
            fontSize: "1rem",
            padding: "0.6rem 1.25rem",
            cursor: "pointer",
            whiteSpace: "nowrap",
          }}
        >
          {posting ? "logging…" : `log it ${selectedMeta?.emoji ?? "🍓"}`}
        </button>
      </div>

      {isLoading ? (
        <p style={{ color: "var(--muted)" }}>loading log…</p>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: "0.75rem" }}>
          {logs.map((log) => <LogEntry key={log.uri} log={log} member={memberMap[log.authorDid]} />)}
        </div>
      )}
    </div>
  );
}

function LogEntry({ log, member }: { log: LogRecord; member?: MemberRecord }) {
  const fruit = getFruit(log.fruit);
  const accentColor = fruit ? `var(${fruit.colorVar})` : "var(--muted)";
  const memberFruitMeta = member?.favoriteFruit ? getFruit(member.favoriteFruit) : undefined;

  return (
    <div style={{
      background: "var(--surface)",
      border: "1px solid var(--border)",
      borderRadius: "var(--radius-card)",
      padding: "1rem 1.25rem",
      display: "flex",
      flexDirection: "column",
      gap: "0.4rem",
    }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <span style={{ fontWeight: 700, color: "var(--text)", fontSize: "0.9rem" }}>
          {memberFruitMeta?.emoji ?? "🍑"} {member?.displayName ?? log.authorDid.slice(0, 12) + "…"}
        </span>
        <span style={{ fontSize: "0.75rem", color: "var(--muted)" }}>
          {new Date(log.createdAt).toLocaleString()}
        </span>
      </div>
      <FruitBurst emoji={fruit?.emoji ?? "🍓"} count={log.count} accentColor={accentColor} />
    </div>
  );
}

function FruitBurst({ emoji, count, accentColor }: { emoji: string; count: number; accentColor: string }) {
  return (
    <div
      style={{
        display: "flex",
        flexWrap: "wrap",
        gap: "2px",
        fontSize: "1.75rem",
        lineHeight: 1,
        padding: "0.25rem 0",
        borderLeft: `3px solid ${accentColor}`,
        paddingLeft: "0.75rem",
      }}
    >
      {Array.from({ length: Math.min(count, 30) }).map((_, i) => (
        <span
          key={i}
          style={{
            display: "inline-block",
            animation: `fruitPop 0.3s ease both`,
            animationDelay: `${i * 40}ms`,
          }}
        >
          {emoji}
        </span>
      ))}
      {count > 30 && (
        <span style={{ fontSize: "1rem", color: accentColor, alignSelf: "center", marginLeft: "4px" }}>
          ×{count}
        </span>
      )}
      <style>{`
        @keyframes fruitPop {
          from { opacity: 0; transform: scale(0.4); }
          to   { opacity: 1; transform: scale(1); }
        }
      `}</style>
    </div>
  );
}
```

- [ ] **Step 2: Verify build passes**

```bash
cd demos/fruitgang/frontend && pnpm build
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add demos/fruitgang/frontend/src/routes/_app/log.tsx
git commit -m "feat(fruitgang): fruit log page with animated emoji burst"
```

---

## Self-Review

**Spec coverage check:**

| Spec requirement | Task |
|-----------------|------|
| Lexicons for member, chat, chatReply, log | Task 1 |
| Custom fruit known values (21 fruits) | Task 1 |
| Backend separate Go module | Task 2 |
| SQLite index with all 4 tables | Task 2 |
| SAP outbox indexer goroutine | Task 3 |
| `/health`, `/getMembers`, `/getChats`, `/getReplies`, `/getLogs` | Task 4 |
| CORS middleware (frontend calls from different origin) | Task 4 |
| Backend cmd/main.go wires SAP + indexer + server | Task 5 |
| `--db` flag with default `fruitgang.db` | Task 5 |
| Moon `fruitgang-backend:dev` task | Task 5 |
| `theme.css` as single design token source | Task 6 |
| Original Surfer + Quicksand fonts | Task 6 |
| `fruits.ts` as single fruit metadata source | Task 6 |
| `__FRUITGANG_API__` injected by vite | Task 6 |
| Caddyfile entries | Task 1 |
| Moon workspace registration | Task 1 |
| `moon r fruitgang:dev` convenience alias | Task 1 |
| Auth via `AuthManager` from `internal` | Task 7 |
| Login page | Task 7 |
| Auth guard (`_app.tsx` beforeLoad) | Task 7 |
| Nav with active tab fruit-color glow | Task 7 |
| Members page grid + profile setup | Task 8 |
| Fruit-color avatar ring per member | Task 8 |
| Chats page with inline flat replies | Task 9 |
| Writes go to pear via `procedure` | Tasks 8, 9, 10 |
| Fruit Log with animated emoji burst | Task 10 |
| Scrollable fruit dropdown with emoji + name | Task 10 |
| Count 1–99 | Task 10 |

All spec requirements covered. No TBDs or placeholders found.
