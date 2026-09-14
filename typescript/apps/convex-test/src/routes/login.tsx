// typescript/apps/convex-test/src/routes/login.tsx
import { createFileRoute } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { useState } from "react";
import { startLogin } from "~/server/sapClient";

const startLoginFn = createServerFn({ method: "POST" })
  .validator((input: { handle: string }) => input)
  .handler(async ({ data }) => ({ redirectUrl: await startLogin(data.handle) }));

export const Route = createFileRoute("/login")({
  component() {
    const [handle, setHandle] = useState("");
    const [pending, setPending] = useState(false);
    const [error, setError] = useState<string | null>(null);

    return (
      <div style={{ maxWidth: 320, margin: "4rem auto" }}>
        <form
          onSubmit={async (e) => {
            e.preventDefault();
            setPending(true);
            setError(null);
            try {
              const { redirectUrl } = await startLoginFn({ data: { handle } });
              window.location.href = redirectUrl;
            } catch (err) {
              setError(String(err));
              setPending(false);
            }
          }}
        >
          <label>
            Handle
            <input
              value={handle}
              onChange={(e) => setHandle(e.target.value)}
              placeholder="alice.bsky.social"
            />
          </label>
          <button type="submit" disabled={pending}>
            Sign In
          </button>
          {error && <p>{error}</p>}
        </form>
      </div>
    );
  },
});
