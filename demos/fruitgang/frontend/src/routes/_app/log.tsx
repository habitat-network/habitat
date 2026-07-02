import { createFileRoute } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { procedure } from "internal";
import { useEffect, useRef, useState } from "react";
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
  const [dropdownOpen, setDropdownOpen] = useState(false);
  const dropdownRef = useRef<HTMLDivElement>(null);

  const { data: logs = [], isLoading } = useQuery({ queryKey: ["logs"], queryFn: fetchLogs, refetchInterval: 4000 });
  const { data: members = [] } = useQuery({ queryKey: ["members"], queryFn: fetchMembers });
  const { data: spaceURI } = useQuery({ queryKey: ["spaceURI"], queryFn: fetchSpaceURI, staleTime: 1000 * 60 * 5 });
  const memberMap = Object.fromEntries(members.map((m) => [m.did, m]));

  // Close dropdown when clicking outside
  useEffect(() => {
    if (!dropdownOpen) return;
    const handler = (e: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setDropdownOpen(false);
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [dropdownOpen]);

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
        padding: "1rem 0",
        marginBottom: "2rem",
        display: "flex",
        gap: "0.75rem",
        alignItems: "center",
        flexWrap: "wrap",
      }}>
        {/* +/− buttons, then emoji display, then ▼ fruit picker */}
        <div style={{ display: "flex", alignItems: "center", gap: "0.4rem" }}>
          <button onClick={() => setCount((c) => Math.max(1, c - 1))} style={stepBtn} aria-label="remove one">➖</button>
          <button onClick={() => setCount((c) => Math.min(99, c + 1))} style={stepBtn} aria-label="add one">➕</button>
        </div>

        <div style={{ fontSize: "1.6rem", letterSpacing: "0.05em", lineHeight: 1, userSelect: "none" }}>
          {selectedMeta?.emoji.repeat(Math.min(count, 20))}{count > 20 ? `…×${count}` : ""}
        </div>

        {/* Inline fruit picker */}
        <div ref={dropdownRef} style={{ position: "relative" }}>
          <button
            onClick={() => setDropdownOpen((o) => !o)}
            style={{
              background: "none",
              border: "none",
              cursor: "pointer",
              fontSize: "1rem",
              color: "var(--text)",
              padding: "0.25rem",
              lineHeight: 1,
            }}
            aria-label="pick fruit"
          >
            ▼
          </button>
          {dropdownOpen && (
            <div style={{
              position: "absolute",
              top: "calc(100% + 4px)",
              left: 0,
              background: "var(--surface)",
              border: "1px solid var(--border)",
              borderRadius: "var(--radius-card)",
              zIndex: 50,
              maxHeight: "260px",
              overflowY: "auto",
              minWidth: "180px",
              boxShadow: "none",
            }}>
              {FRUIT_KEYS.map((key) => (
                <button
                  key={key}
                  onClick={() => { setSelectedFruit(key); setDropdownOpen(false); }}
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: "0.5rem",
                    width: "100%",
                    background: key === selectedFruit ? "var(--bg)" : "none",
                    border: "none",
                    borderBottom: "1px solid var(--border)",
                    color: "var(--text)",
                    fontFamily: "var(--font-body)",
                    fontSize: "0.9rem",
                    padding: "0.5rem 0.75rem",
                    cursor: "pointer",
                    textAlign: "left",
                  }}
                >
                  <span>{FRUITS[key].emoji}</span>
                  <span>{FRUITS[key].label}</span>
                </button>
              ))}
            </div>
          )}
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
      <span style={{ fontSize: "0.75rem", color: "var(--muted)", whiteSpace: "nowrap", flexShrink: 0 }}>{date}</span>
    </div>
  );
}

const stepBtn: React.CSSProperties = {
  background: "none",
  border: "none",
  color: "var(--text)",
  fontSize: "1.3rem",
  cursor: "pointer",
  lineHeight: 1,
  padding: 0,
};
