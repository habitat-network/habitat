import { createFileRoute } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { procedure } from "internal";
import { useRef, useState } from "react";
import { FRUITS, FRUIT_KEYS, getFruit } from "@/fruits";

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

const fetchSpaceURI = async (): Promise<string | null> => {
  const res = await fetch(`${__FRUITGANG_API__}/getSpaceURI`);
  if (res.status === 404) return null;
  if (!res.ok) throw new Error("fetch space URI failed");
  return ((await res.json()) as { uri: string }).uri;
};

// ── placement algorithm ────────────────────────────────────────────────────

const CARD_W = 260;
const GAP = 24;       // minimum spacing between cards
const BOARD_W = 780;  // usable width (content area minus scroll bar margin)

type Rect = { x: number; y: number; w: number; h: number };
type ChatPlacement = { left: number; top: number; pinEmoji: string; bgVar: string; textColor: string };

const DARK_FRUIT_VARS = new Set(["--strawberry", "--grape", "--blueberry", "--cherry", "--muted"]);

const CARD_COLOR_VARS = [...new Set(
  FRUIT_KEYS.map((k) => FRUITS[k].colorVar).filter((v) => v !== "--lime")
)];

function randomFruitColor(): { bgVar: string; textColor: string } {
  const bgVar = CARD_COLOR_VARS[Math.floor(Math.random() * CARD_COLOR_VARS.length)];
  const textColor = DARK_FRUIT_VARS.has(bgVar) ? "var(--light-text)" : "var(--text)";
  return { bgVar, textColor };
}

function estimateCardHeight(text: string): number {
  // pin emoji row + author/date row + text rows + replies button
  const charsPerLine = 34;
  const textLines = Math.ceil((text.length || 1) / charsPerLine);
  return 40 + 24 + textLines * 22 + 32;
}

function overlaps(a: Rect, b: Rect): boolean {
  return (
    a.x < b.x + b.w + GAP && a.x + a.w + GAP > b.x &&
    a.y < b.y + b.h + GAP && a.y + a.h + GAP > b.y
  );
}

function pickPosition(w: number, h: number, placed: Rect[]): { x: number; y: number } {
  const maxX = BOARD_W - w;
  const currentBottom = placed.reduce((m, r) => Math.max(m, r.y + r.h), 0);

  // Try random positions within the current content area (+ some slack below)
  for (let attempt = 0; attempt < 300; attempt++) {
    const x = Math.floor(Math.random() * maxX);
    const y = Math.floor(Math.random() * Math.max(80, currentBottom + 80));
    const rect = { x, y, w, h };
    if (!placed.some((p) => overlaps(rect, p))) return { x, y };
  }

  // Fallback: scan downward in small increments until a gap is found
  for (let y = 0; y < 6000; y += 8) {
    const x = Math.floor(Math.random() * maxX);
    const rect = { x, y, w, h };
    if (!placed.some((p) => overlaps(rect, p))) return { x, y };
  }

  return { x: 0, y: currentBottom + GAP };
}

function randomPinEmoji(): string {
  return FRUITS[FRUIT_KEYS[Math.floor(Math.random() * FRUIT_KEYS.length)]].emoji;
}

// ── component ─────────────────────────────────────────────────────────────

export const Route = createFileRoute("/_app/chats")({
  component: ChatsPage,
});

function ChatsPage() {
  const { authManager } = Route.useRouteContext();
  const qc = useQueryClient();
  const did = authManager.getAuthInfo()?.did ?? "";
  const [newText, setNewText] = useState("");
  const [expandedUri, setExpandedUri] = useState<string | null>(null);

  const { data: chats = [], isLoading } = useQuery({ queryKey: ["chats"], queryFn: fetchChats, refetchInterval: 4000 });
  const { data: members = [] } = useQuery({ queryKey: ["members"], queryFn: fetchMembers });
  const { data: spaceURI } = useQuery({ queryKey: ["spaceURI"], queryFn: fetchSpaceURI, staleTime: 1000 * 60 * 5 });
  const memberMap = Object.fromEntries(members.map((m) => [m.did, m]));

  // Stable placements: computed once per URI, new chats placed into remaining gaps
  const boardRef = useRef<{ placements: Map<string, ChatPlacement>; rects: Rect[] }>({
    placements: new Map(),
    rects: [],
  });
  for (const chat of chats) {
    if (!boardRef.current.placements.has(chat.uri)) {
      const h = estimateCardHeight(chat.text);
      const { x, y } = pickPosition(CARD_W, h, boardRef.current.rects);
      boardRef.current.rects.push({ x, y, w: CARD_W, h });
      boardRef.current.placements.set(chat.uri, { left: x, top: y, pinEmoji: randomPinEmoji(), ...randomFruitColor() });
    }
  }

  const boardHeight = boardRef.current.rects.reduce((m, r) => Math.max(m, r.y + r.h + 60), 400);

  const { mutate: postChat, isPending: posting } = useMutation({
    mutationFn: async (text: string) => {
      await procedure("network.habitat.space.putRecord", {
        space: spaceURI ?? undefined,
        collection: "community.fruitgang.chat",
        record: { text, createdAt: new Date().toISOString() },
      }, { authManager });
    },
    onSuccess: () => { setNewText(""); qc.invalidateQueries({ queryKey: ["chats"] }); },
  });

  return (
    <div>
      <h2 style={{ fontFamily: "var(--font-display)", fontSize: "2rem", color: "var(--strawberry)", marginBottom: "1.5rem" }}>
        fruit chats 🍒
      </h2>

      <div style={{
        marginBottom: "1.5rem",
        display: "flex",
        gap: "0.75rem",
        alignItems: "flex-end",
        paddingBottom: "0.75rem",
      }}>
        <textarea
          placeholder="say something fruity…"
          value={newText}
          maxLength={300}
          onChange={(e) => setNewText(e.target.value)}
          rows={2}
          style={{
            flex: 1, resize: "none",
            background: "transparent",
            border: "none",
            color: "var(--cherry)",
            padding: "0.25rem 0",
            fontFamily: "var(--font-body)",
            fontSize: "0.9rem",
            outline: "none",
          }}
        />
        <button
          onClick={() => newText.trim() && postChat(newText.trim())}
          disabled={posting || !newText.trim()}
          style={{
            background: "var(--peach)",
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
        <div style={{ position: "relative", minHeight: boardHeight }}>
          {chats.map((chat) => {
            const placement = boardRef.current.placements.get(chat.uri);
            if (!placement) return null;
            const isExpanded = expandedUri === chat.uri;
            return (
              <div
                key={chat.uri}
                style={{
                  position: "absolute",
                  left: placement.left,
                  top: placement.top,
                  width: CARD_W,
                  zIndex: isExpanded ? 20 : 1,
                }}
              >
                <ChatItem
                  chat={chat}
                  member={memberMap[chat.authorDid]}
                  memberMap={memberMap}
                  pinEmoji={placement.pinEmoji}
                  bgVar={placement.bgVar}
                  cardTextColor={placement.textColor}
                  isExpanded={isExpanded}
                  onExpand={() => setExpandedUri((u) => (u === chat.uri ? null : chat.uri))}
                  authManager={authManager}
                  currentDid={did}
                  spaceURI={spaceURI}
                  onReplyPosted={() => qc.invalidateQueries({ queryKey: ["replies", chat.uri] })}
                />
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

function ChatItem({
  chat, member, memberMap, pinEmoji, bgVar, cardTextColor, isExpanded, onExpand, authManager, currentDid, onReplyPosted, spaceURI,
}: {
  chat: ChatRecord;
  member?: MemberRecord;
  memberMap: Record<string, MemberRecord>;
  pinEmoji: string;
  bgVar: string;
  cardTextColor: string;
  isExpanded: boolean;
  onExpand: () => void;
  authManager: any;
  currentDid: string;
  onReplyPosted: () => void;
  spaceURI?: string | null;
}) {
  const fruit = member?.favoriteFruit ? getFruit(member.favoriteFruit) : undefined;
  const accentColor = fruit ? `var(${fruit.colorVar})` : "var(--muted)";
  const [replyText, setReplyText] = useState("");
  const qc = useQueryClient();

  const { data: replies = [] } = useQuery({
    queryKey: ["replies", chat.uri],
    queryFn: () => fetchReplies(chat.uri),
    enabled: isExpanded,
    refetchInterval: isExpanded ? 4000 : false,
  });

  const { mutate: postReply, isPending: replying } = useMutation({
    mutationFn: async (text: string) => {
      await procedure("network.habitat.space.putRecord", {
        space: spaceURI ?? undefined,
        collection: "community.fruitgang.chatReply",
        record: { text, replyTo: chat.uri, createdAt: new Date().toISOString() },
      }, { authManager });
    },
    onSuccess: () => { setReplyText(""); qc.invalidateQueries({ queryKey: ["replies", chat.uri] }); onReplyPosted(); },
  });

  const mutedTextColor = cardTextColor === "var(--light-text)" ? "rgba(255,255,255,0.7)" : "var(--muted)";

  return (
    <div style={{
      background: `var(${bgVar})`,
      border: "1px solid var(--border)",
      borderRadius: "var(--radius-card)",
      padding: "1rem",
    }}>
      <div style={{ fontSize: "1.4rem", lineHeight: 1, marginBottom: "0.5rem" }}>{pinEmoji}</div>
      <div style={{ display: "flex", justifyContent: "space-between", marginBottom: "0.4rem", gap: "0.5rem" }}>
        <span style={{ fontWeight: 700, color: cardTextColor, fontSize: "0.85rem" }}>
          {member?.displayName ?? chat.authorDid.slice(0, 12) + "…"}
        </span>
        <span style={{ fontSize: "0.75rem", color: mutedTextColor, whiteSpace: "nowrap" }}>
          {new Date(chat.createdAt).toLocaleDateString("en-US", { month: "short", day: "numeric" })}
        </span>
      </div>
      <p style={{ margin: "0 0 0.5rem", color: cardTextColor, lineHeight: 1.5, fontSize: "0.88rem", maxHeight: "8rem", overflowY: "auto" }}>{chat.text}</p>
      <button
        onClick={onExpand}
        style={{
          background: "none", border: "none", color: mutedTextColor, fontSize: "0.78rem",
          cursor: "pointer", fontFamily: "var(--font-body)", padding: 0,
        }}
      >
        {isExpanded ? "▲ hide replies" : "▼ replies"}
      </button>

      {isExpanded && (
        <div style={{ marginTop: "0.75rem", paddingLeft: "0.75rem", borderLeft: `2px solid ${accentColor}`, display: "flex", flexDirection: "column", gap: "0.5rem" }}>
          {replies.map((r) => {
            const replyMember = memberMap[r.authorDid];
            const replyFruit = replyMember?.favoriteFruit ? getFruit(replyMember.favoriteFruit) : undefined;
            return (
              <div key={r.uri} style={{ fontSize: "0.85rem", color: mutedTextColor }}>
                <strong style={{ color: cardTextColor }}>
                  {replyFruit?.emoji ?? "🍑"} {replyMember?.displayName ?? r.authorDid.slice(0, 12) + "…"}
                </strong>{" "}
                <span style={{ display: "block", maxHeight: "5rem", overflowY: "auto", marginTop: "0.2rem" }}>{r.text}</span>
              </div>
            );
          })}
          <div style={{ display: "flex", gap: "0.5rem", marginTop: "0.25rem" }}>
            <input
              placeholder="reply…"
              value={replyText}
              maxLength={300}
              onChange={(e) => setReplyText(e.target.value)}
              style={{
                flex: 1,
                background: "transparent",
                border: "none",
                borderBottom: `1px solid ${mutedTextColor}`,
                color: cardTextColor,
                padding: "0.25rem 0",
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
