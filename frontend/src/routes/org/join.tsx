import { Button, Input } from "internal";
import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";

export const Route = createFileRoute("/org/join")({
  validateSearch: (search: Record<string, unknown>) => ({
    token: String(search.token ?? ""),
  }),
  component: JoinPage,
});

function JoinPage() {
  const { token } = Route.useSearch();
  const [handle, setHandle] = useState("");
  const [result, setResult] = useState<{ handle: string; did: string } | null>(
    null,
  );
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setLoading(true);
    setError(null);
    try {
      // TODO: is this the right way to target habitat domain ?
      const res = await fetch(`https://${__HABITAT_DOMAIN__}/xrpc/network.habitat.org.mintMemberIdentity`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token, handle }),
      });
      if (!res.ok) {
        const data = await res.json().catch(() => ({}));
        throw new Error(
          (data as { message?: string }).message ?? "Failed to join organization",
        );
      }
      setResult(await res.json());
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unknown error");
    } finally {
      setLoading(false);
    }
  }

  if (result) {
    return (
      <div className="flex flex-col gap-4 max-w-md mx-auto mt-16">
        <h1 className="text-2xl font-semibold">Welcome!</h1>
        <p className="text-muted-foreground">Your account has been created.</p>
        <div className="flex flex-col gap-1 text-sm font-mono">
          <span>{result.handle}</span>
          <span className="text-muted-foreground">{result.did}</span>
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4 max-w-md mx-auto mt-16">
      <h1 className="text-2xl font-semibold">Join Organization</h1>
      <p className="text-muted-foreground text-sm">
        Choose a handle for your new account.
      </p>
      <form onSubmit={handleSubmit} className="flex flex-col gap-3">
        <Input
          placeholder="handle"
          value={handle}
          onChange={(e) => setHandle(e.target.value)}
          disabled={loading}
        />
        {error && <p className="text-sm text-destructive">{error}</p>}
        <Button type="submit" disabled={!handle || loading}>
          {loading ? "Joining..." : "Join"}
        </Button>
      </form>
    </div>
  );
}
