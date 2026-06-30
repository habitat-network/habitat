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
      await procedure("network.habitat.space.putRecord", {
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
            cursor: "pointer",
          }}
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
