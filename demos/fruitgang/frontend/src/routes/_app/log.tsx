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

const fetchSpaceURI = async (): Promise<string | null> => {
  const res = await fetch(`${__FRUITGANG_API__}/getSpaceURI`);
  if (res.status === 404) return null;
  if (!res.ok) throw new Error("fetch space URI failed");
  return ((await res.json()) as { uri: string }).uri;
};

export const Route = createFileRoute("/_app/log")({
  component: LogPage,
});

function LogPage() {
  const { authManager } = Route.useRouteContext();
  const qc = useQueryClient();

  const [selectedFruit, setSelectedFruit] = useState("strawberry");
  const [count, setCount] = useState(1);

  const { data: logs = [], isLoading } = useQuery({ queryKey: ["logs"], queryFn: fetchLogs, refetchInterval: 4000 });
  const { data: members = [] } = useQuery({ queryKey: ["members"], queryFn: fetchMembers });
  const { data: spaceURI } = useQuery({ queryKey: ["spaceURI"], queryFn: fetchSpaceURI, staleTime: 1000 * 60 * 5 });
  const memberMap = Object.fromEntries(members.map((m) => [m.did, m]));

  const { mutate: postLog, isPending: posting } = useMutation({
    mutationFn: async () => {
      await procedure("network.habitat.space.putRecord", {
        space: spaceURI ?? undefined,
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
        padding: "1rem 1.25rem",
        marginBottom: "2rem",
        display: "flex",
        gap: "0.75rem",
        alignItems: "center",
        flexWrap: "wrap",
      }}>
        <select
          value={selectedFruit}
          onChange={(e) => { setSelectedFruit(e.target.value); setCount(1); }}
          style={{
            background: "var(--surface)",
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

        <div style={{ display: "flex", alignItems: "center", gap: "0.5rem", flex: 1, flexWrap: "wrap" }}>
          <div style={{ fontSize: "1.6rem", letterSpacing: "0.05em", lineHeight: 1 }}>
            {selectedMeta?.emoji.repeat(Math.min(count, 20))}{count > 20 ? `…×${count}` : ""}
          </div>
          <div style={{ display: "flex", gap: "0.25rem", marginLeft: "0.25rem" }}>
            {count > 1 && (
              <button
                onClick={() => setCount((c) => c - 1)}
                style={stepBtn}
                aria-label="remove one"
              >−</button>
            )}
            <button
              onClick={() => setCount((c) => Math.min(99, c + 1))}
              style={stepBtn}
              aria-label="add one"
            >+</button>
          </div>
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
  const name = member?.displayName ?? log.authorDid.slice(0, 12) + "…";
  const date = new Date(log.createdAt).toLocaleDateString("en-US", { month: "short", day: "numeric" });

  return (
    <div style={{ display: "flex", justifyContent: "space-between", alignItems: "baseline", gap: "1rem", padding: "0.3rem 0" }}>
      <span style={{ color: "var(--text)", fontSize: "0.95rem" }}>
        <span style={{ fontWeight: 700 }}>{name}</span>
        {" logged "}
        {fruit?.emoji.repeat(Math.min(log.count, 99)) ?? "🍓"}
      </span>
      <span style={{ fontSize: "0.75rem", color: "var(--lime)", whiteSpace: "nowrap", flexShrink: 0 }}>{date}</span>
    </div>
  );
}

const stepBtn: React.CSSProperties = {
  background: "var(--surface)",
  border: "1px solid var(--border)",
  borderRadius: "var(--radius-input)",
  color: "var(--text)",
  fontFamily: "var(--font-body)",
  fontSize: "1.1rem",
  width: "2rem",
  height: "2rem",
  cursor: "pointer",
  display: "flex",
  alignItems: "center",
  justifyContent: "center",
  lineHeight: 1,
  padding: 0,
};
