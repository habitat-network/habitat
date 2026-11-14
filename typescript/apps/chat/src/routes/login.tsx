import { createFileRoute } from "@tanstack/react-router";
import { SignInForm } from "internal";
import { z } from "zod";
import { startLogin } from "@/server/functions";
import { ChatMark } from "@/components/ChatMark";

export const Route = createFileRoute("/login")({
  validateSearch: z.object({ error: z.string().optional() }),
  component() {
    const { error } = Route.useSearch();
    return (
      <div className="flex min-h-svh flex-col items-center bg-muted/40">
        <div className="mt-24 flex flex-col items-center gap-3 text-center">
          <ChatMark className="size-12" />
          <h1 className="text-2xl font-semibold tracking-tight">Chat</h1>
          <p className="max-w-sm text-sm text-muted-foreground">
            Channels for your organization. Every message is a record in a space
            your org owns, readable only by the channel's members.
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
