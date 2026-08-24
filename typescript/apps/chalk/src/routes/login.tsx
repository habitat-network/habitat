import { createFileRoute } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { useState } from "react";
import { startLogin } from "@/server/sapClient";

// startLogin itself isn't a TanStack server function (it's a plain async
// function in sapClient.ts, shared with docSync/functions.ts's composition
// root) — wrap it here at the route boundary so the client only ever gets
// an RPC stub, never sap's internal URL or the fetch call itself.
const startLoginFn = createServerFn({ method: "POST" })
  .validator((input: { handle: string }) => input)
  .handler(async ({ data }) => ({
    redirectUrl: await startLogin(data.handle),
  }));

export const Route = createFileRoute("/login")({
  component() {
    const [handle, setHandle] = useState("");
    const [pending, setPending] = useState(false);
    const [error, setError] = useState<string>();

    return (
      <div className="h-full w-full flex items-center justify-center">
        <form
          className="flex flex-col gap-3 w-72"
          onSubmit={async (e) => {
            e.preventDefault();
            setPending(true);
            setError(undefined);
            try {
              const { redirectUrl } = await startLoginFn({ data: { handle } });
              window.location.href = redirectUrl;
            } catch (err) {
              setError(err instanceof Error ? err.message : String(err));
              setPending(false);
            }
          }}
        >
          <h1 className="text-lg font-medium">Sign In</h1>
          <label className="flex flex-col gap-1">
            <span>Handle</span>
            <input
              className="border rounded px-2 py-1"
              value={handle}
              onChange={(e) => setHandle(e.target.value)}
              required
            />
          </label>
          {error && <p className="text-red-600 text-sm">{error}</p>}
          <button
            type="submit"
            disabled={pending}
            className="border rounded px-3 py-1.5 bg-black text-white disabled:opacity-50"
          >
            {pending ? "Signing in..." : "Sign In"}
          </button>
        </form>
      </div>
    );
  },
});
