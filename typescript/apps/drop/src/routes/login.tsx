import { createFileRoute } from "@tanstack/react-router";
import { SignInForm } from "internal";
import { z } from "zod";
import { startLogin } from "@/server/functions";
import { DropMark } from "@/components/DropMark";

export const Route = createFileRoute("/login")({
  validateSearch: z.object({ error: z.string().optional() }),
  component() {
    const { error } = Route.useSearch();
    return (
      <div className="flex min-h-svh flex-col items-center bg-muted/40">
        <div className="mt-24 flex flex-col items-center gap-3 text-center">
          <DropMark className="size-12" />
          <h1 className="text-2xl font-semibold tracking-tight">Drop</h1>
          <p className="max-w-sm text-sm text-muted-foreground">
            A shared file bucket for your organization. Everything you drop here
            is searchable by everyone in the org.
          </p>
        </div>
        <div className="w-full [&>div]:py-10">
          <SignInForm
            serverError={error}
            onSubmit={async (handle) => {
              const { redirectUrl } = await startLogin({ data: { handle } });
              window.location.href = redirectUrl;
              // Keep the button in its loading state while the browser navigates.
              await new Promise(() => {});
            }}
          />
        </div>
      </div>
    );
  },
});
