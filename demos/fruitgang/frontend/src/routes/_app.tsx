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
