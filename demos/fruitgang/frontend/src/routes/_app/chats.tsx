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
      await procedure("network.habitat.space.putRecord", {
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
      await procedure("network.habitat.space.putRecord", {
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
