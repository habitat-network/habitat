import { createFileRoute, Link, Outlet, redirect } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { getFruit } from "@/fruits";

async function fetchMembers(): Promise<Array<{ did: string; displayName?: string; favoriteFruit?: string }>> {
  const res = await fetch(`${__FRUITGANG_API__}/getMembers`);
  if (!res.ok) return [];
  return res.json();
}

export async function fetchSpaceURI(): Promise<string | null> {
  const res = await fetch(`${__FRUITGANG_API__}/getSpaceURI`);
  if (res.status === 404) return null;
  if (!res.ok) throw new Error("fetch space URI failed");
  const data = await res.json();
  return data.uri as string;
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

  const { data: spaceURI, isLoading: spaceLoading } = useQuery({
    queryKey: ["spaceURI"],
    queryFn: fetchSpaceURI,
    staleTime: 1000 * 60 * 5,
    retry: false,
  });

  const { data: members = [] } = useQuery({
    queryKey: ["members"],
    queryFn: fetchMembers,
    staleTime: 1000 * 30,
  });

  const me = members.find((m) => m.did === did);
  const myFruit = me?.favoriteFruit;
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
        <div style={{
          display: "flex",
          alignItems: "baseline",
          gap: "0.6rem",
          marginRight: "auto",
          userSelect: "none",
        }}>
          <span style={{
            fontFamily: "var(--font-display)",
            fontSize: "1.4rem",
            color: "var(--text)",
          }}>
            🍓 fruit gang
          </span>
          <span style={{
            fontFamily: "var(--font-body)",
            fontSize: "0.8rem",
            color: "var(--muted)",
          }}>
            a community for fruit lovers
          </span>
        </div>

        {spaceURI && (
          <>
            <NavTab to="/members" label="Members" accentColor={accentColor} />
            <NavTab to="/chats" label="Chats" accentColor={accentColor} />
            <NavTab to="/log" label="Log" accentColor={accentColor} />
          </>
        )}

        {me?.displayName && (
          <span style={{
            marginLeft: "auto",
            fontFamily: "var(--font-body)",
            fontWeight: 600,
            fontSize: "0.85rem",
            color: "var(--text)",
          }}>
            {me.displayName}
          </span>
        )}

        <button
          onClick={() => authManager.logout()}
          style={{
            marginLeft: me?.displayName ? undefined : "auto",
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
        {spaceLoading ? null : spaceURI ? (
          <Outlet />
        ) : (
          <PendingApproval authManager={authManager} />
        )}
      </main>
    </div>
  );
}

function PendingApproval({ authManager }: { authManager: any }) {
  const [handle, setHandle] = useState("");
  const [approving, setApproving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleApprove = async () => {
    if (!handle.trim()) return;
    setApproving(true);
    setError(null);
    try {
      const res = await fetch(`${__FRUITGANG_API__}/add-org`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ handle: handle.trim() }),
      });
      if (!res.ok) {
        const text = await res.text();
        setError(text || "failed to start approval");
        return;
      }
      const { redirect_url } = await res.json() as { redirect_url: string };
      window.location.href = redirect_url;
    } catch (e) {
      setError(String(e));
    } finally {
      setApproving(false);
    }
  };

  return (
    <div style={{
      display: "flex",
      flexDirection: "column",
      alignItems: "center",
      justifyContent: "center",
      minHeight: "60vh",
      gap: "1.25rem",
      textAlign: "center",
    }}>
      <span style={{ fontSize: "3rem" }}>🍑</span>
      <h2 style={{ fontFamily: "var(--font-display)", fontSize: "1.75rem", color: "var(--text)", margin: 0 }}>
        waiting for approval
      </h2>
      <p style={{ color: "var(--muted)", maxWidth: "400px", lineHeight: 1.6, margin: 0 }}>
        Your organization hasn't connected to Fruit Gang yet.
      </p>

      <div style={{
        marginTop: "0.5rem",
        background: "var(--surface)",
        border: "1px solid var(--border)",
        borderRadius: "var(--radius-card)",
        padding: "1.25rem 1.5rem",
        maxWidth: "380px",
        width: "100%",
        display: "flex",
        flexDirection: "column",
        gap: "0.75rem",
      }}>
        <p style={{ color: "var(--muted)", fontSize: "0.85rem", margin: 0 }}>
          If you're an org admin, enter your org handle to connect it.
        </p>
        <input
          placeholder="org handle (e.g. myorg.habitat.network)"
          value={handle}
          onChange={(e) => setHandle(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && handleApprove()}
          style={{
            background: "transparent",
            border: "1px solid var(--border)",
            borderRadius: "var(--radius-input)",
            color: "var(--text)",
            padding: "0.6rem 0.9rem",
            fontFamily: "var(--font-body)",
            fontSize: "0.9rem",
            outline: "none",
            width: "100%",
            boxSizing: "border-box",
          }}
        />
        <button
          onClick={handleApprove}
          disabled={approving || !handle.trim()}
          style={{
            background: "var(--strawberry)",
            border: "none",
            borderRadius: "var(--radius-pill)",
            color: "#000",
            fontFamily: "var(--font-display)",
            fontSize: "1rem",
            padding: "0.6rem 1.25rem",
            cursor: approving || !handle.trim() ? "default" : "pointer",
            fontWeight: 700,
            opacity: approving || !handle.trim() ? 0.6 : 1,
          }}
        >
          {approving ? "redirecting…" : "connect org 🍓"}
        </button>
        {error && <p style={{ color: "var(--strawberry)", fontSize: "0.8rem", margin: 0 }}>{error}</p>}
      </div>

      <button
        onClick={() => authManager.logout()}
        style={{
          background: "none",
          border: "1px solid var(--border)",
          color: "var(--muted)",
          borderRadius: "var(--radius-pill)",
          padding: "0.4rem 1.1rem",
          cursor: "pointer",
          fontFamily: "var(--font-body)",
          fontSize: "0.85rem",
        }}
      >
        sign out
      </button>
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
          fontFamily: "var(--font-body)",
          fontWeight: 600,
          fontSize: "0.9rem",
          color: isActive ? "var(--text)" : "var(--muted)",
          borderBottom: isActive ? `2px solid ${accentColor}` : "2px solid transparent",
        }}>
          {label}
        </span>
      )}
    </Link>
  );
}
