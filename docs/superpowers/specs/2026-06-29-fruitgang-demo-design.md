# Fruit Gang Demo App — Design Spec

**Date:** 2026-06-29
**Status:** Approved

## Overview

A local demo app built on top of Habitat demonstrating community spaces. "Fruit Gang" is a playful community where members log fruit consumption, post chats, and maintain profiles. It runs entirely locally via `moon r fruitgang:dev`.

The app lives at `demos/fruitgang/` with a Go backend and a TypeScript frontend as siblings.

---

## Lexicons

All lexicons live under `lexicons/community/fruitgang/` and follow the AT Protocol lexicon format.

### `community.fruitgang.member`
Record (`key: tid`) — a member's Fruit Gang profile.

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `displayName` | string | yes | |
| `avatar` | blob | no | |
| `funFact` | string | no | |
| `favoriteFruit` | string (known values) | no | Fruit token slugs e.g. `community.fruitgang.member#strawberry` |
| `createdAt` | datetime | yes | |

### `community.fruitgang.chat`
Record (`key: tid`) — a top-level community post.

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `text` | string (maxLength: 300) | yes | |
| `createdAt` | datetime | yes | |

### `community.fruitgang.chatReply`
Record (`key: tid`) — a flat reply to a chat.

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `text` | string (maxLength: 300) | yes | |
| `replyTo` | string (format: at-uri) | yes | Points to a `community.fruitgang.chat` record |
| `createdAt` | datetime | yes | |

### `community.fruitgang.log`
Record (`key: tid`) — a fruit log entry.

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `fruit` | string (known values) | yes | Fruit token slugs |
| `count` | integer (min: 1, max: 99) | yes | |
| `createdAt` | datetime | yes | |

### Fruit known values
All four lexicons with fruit fields share the same set of known values (tokens defined on the respective lexicon). The full list covers every fruit with an emoji:

🍏 Green Apple, 🍎 Red Apple, 🍐 Pear, 🍊 Tangerine, 🍋 Lemon, 🍋‍🟩 Lime, 🍌 Banana, 🍉 Watermelon, 🍇 Grapes, 🍓 Strawberry, 🫐 Blueberry, 🍈 Melon, 🍒 Cherries, 🍑 Peach, 🥭 Mango, 🍍 Pineapple, 🥥 Coconut, 🥝 Kiwi, 🍅 Tomato, 🫒 Olive, 🥑 Avocado

---

## Backend (`demos/fruitgang/backend`)

### Structure

```
demos/fruitgang/backend/
  cmd/
    main.go          # entrypoint: CLI flags, wires SAP + HTTP server
  internal/
    index/
      models.go      # GORM models for SQLite index tables
      store.go       # read/write helpers for the index
    server/
      server.go      # HTTP routes
    indexer/
      indexer.go     # goroutine: drains SAP outbox → writes to SQLite index
  moon.yml
  go.mod             # separate Go module: module demos/fruitgang
```

### Approach

The backend embeds SAP (imported from the root module) and runs it in-process. A dedicated `indexer` goroutine polls `sap.Outbox.Poll()`, decodes each record by collection NSID, upserts it into the appropriate SQLite table, and acks the message.

### Config (flags / env)

| Flag | Default | Description |
|------|---------|-------------|
| `--db` | `fruitgang.db` | SQLite path, prefix `sqlite://` |
| `--port` | `3100` | HTTP listen port |
| `--pear-url` | `https://pear.local.habitat.network` | Pear server URL |
| `--org-did` | (required) | The Fruit Gang org DID to register with SAP |
| `--secret` | (required) | AT crypto private key for SAP OAuth |

### SQLite index tables (GORM)

- `members` — DID, URI, display_name, avatar_cid, fun_fact, favorite_fruit, created_at, indexed_at
- `chats` — URI, author_did, text, created_at, indexed_at
- `chat_replies` — URI, author_did, reply_to (AT URI), text, created_at, indexed_at
- `logs` — URI, author_did, fruit, count, created_at, indexed_at

All tables have `uri` as primary key. Upsert on conflict replaces all fields (handles edits/resyncs).

### HTTP routes

All routes return JSON. No auth required for reads.

| Method | Path | Response |
|--------|------|----------|
| GET | `/health` | `{"ok": true}` |
| GET | `/getMembers` | `[]{did, uri, displayName, avatarCid, funFact, favoriteFruit, createdAt}` |
| GET | `/getChats` | `[]{uri, authorDid, text, createdAt}` newest-first |
| GET | `/getReplies?chatUri=<at-uri>` | `[]{uri, authorDid, text, createdAt}` oldest-first |
| GET | `/getLogs` | `[]{uri, authorDid, fruit, count, createdAt}` newest-first |

### Moon task

```yaml
# demos/fruitgang/backend/moon.yml
layer: application
tasks:
  dev:
    command: go run ./cmd
    deps:
      - pear:dev
    env:
      PORT: "3100"
    preset: server
```

---

## Frontend (`demos/fruitgang/frontend`)

### Structure

```
demos/fruitgang/frontend/
  src/
    theme.css        # ALL color tokens, font imports, Tailwind @theme block
    fruits.ts        # fruit slug → { emoji, label, color token } map
    main.tsx
    routes/
      __root.tsx
      login.tsx
      _app.tsx       # auth guard + nav layout
      _app/
        members.tsx
        chats.tsx
        log.tsx
  index.html
  package.json
  moon.yml
  vite.config.ts
  tsconfig.json
```

### Auth

Reuses `AuthManager` from `internal` for OAuth login/logout. The `_app.tsx` layout route redirects to `/login` if unauthenticated (same `beforeLoad` pattern as the docs app).

### Pages

**`/members`** — Responsive card grid. Each card shows: avatar with fruit-color ring, display name, handle, favorite fruit emoji, fun fact, member-since. Reads from `GET /getMembers`.

**`/chats`** — Scrollable feed of top-level posts, newest first. Each post shows author avatar + name + text + timestamp + reply count. Clicking a post expands inline replies beneath it. Compose boxes for new posts and replies write to pear via `procedure("network.habitat.repo.putRecord", ...)`. Reads from `GET /getChats` and `GET /getReplies`.

**`/log`** — Reverse-chronological log entries. Each entry: author avatar + name, fruit emojis repeated `count` times at `2rem` with a stagger-in animation, timestamp. Compose: scrollable dropdown (fruit emoji + name) + number input (1–99) + submit. Writes to pear. Reads from `GET /getLogs`.

### Visual identity

**Theme file:** `src/theme.css` is the single source of truth for all design tokens — imported once in `main.tsx`. All components reference CSS variables; no hardcoded color values anywhere else.

**Palette:**

```css
/* src/theme.css */
--strawberry:      #FF2D55;
--tangerine:       #FF9F0A;
--banana:          #FFD60A;
--lime:            #30D158;
--grape:           #BF5AF2;
--blueberry:       #0A84FF;
--cherry:          #FF375F;
--peach:           #FFB340;

--bg:              #09090F;
--surface:         #13131F;
--surface-raised:  #1C1C2E;
--text:            #F5F5F7;
--muted:           #6E6E80;
--border:          #2A2A40;
```

**Typography:**

```css
--font-display: 'Original Surfer', cursive;   /* wordmark, nav tabs, headings */
--font-body:    'Quicksand', sans-serif;       /* all other text */
```

Both loaded from Google Fonts in `theme.css` via `@import`.

**Fruit → color mapping (`src/fruits.ts`):**

A single exported map: `FRUITS` keyed by lexicon token slug → `{ emoji, label, colorVar }` where `colorVar` is a CSS variable name (e.g. `--strawberry`). This is the only place fruit metadata lives; all UI components import from here.

**Signature element:** Each member is assigned a fruit accent color via their `favoriteFruit` (looked up in `FRUITS`). That color appears as:
- A `2px` ring + soft radial glow on their avatar
- A left-border accent on their chat posts
- A subtle tinted background on their member card

Log entries animate in with a stagger: each repeated fruit emoji scales from `0.5` to `1` with a 60ms delay per emoji, giving the impression of fruit landing one by one.

**Nav:** Top bar with `🍓 fruit gang` wordmark in Original Surfer (left), three tab pills (Members / Chats / Log) centered, user avatar (right). Active tab pill has a soft glow in the logged-in user's fruit accent color.

### Moon task

```yaml
# demos/fruitgang/frontend/moon.yml
layer: application
tasks:
  dev:
    env:
      SERVER_PORT: "5178"
    deps:
      - fruitgang-backend:dev
      - root:caddy
```

### Caddyfile addition

```
fruitgang.local.habitat.network {
  reverse_proxy 127.0.0.1:5178
  tls internal
}
```

---

## Moon workspace

A top-level `demos/fruitgang/moon.yml` (or entries in `.moon/workspace.yml`) registers both `fruitgang-backend` and `fruitgang-frontend` as Moon projects. `moon r fruitgang:dev` is a convenience alias that starts both.

---

## What is NOT in scope

- No real-time updates / WebSocket push (polling on TanStack Query refetch interval is sufficient for a local demo)
- No image upload UI for avatars (field exists in lexicon, UI shows placeholder if absent)
- No moderation, deletion, or editing of posts
- No pagination (local demo, small dataset)
