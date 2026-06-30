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
