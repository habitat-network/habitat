import { createFileRoute } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { EMAIL_DOMAIN_NOT_FOUND_MESSAGE, SignInForm } from "internal";
import { env } from "cloudflare:workers";
import { EmailDomainNotFoundError, startLogin } from "@/server/sapClient";

// startLogin itself isn't a TanStack server function (it's a plain async
// function in sapClient.ts, shared with functions.ts's composition root) —
// wrap it here at the route boundary so the client only ever gets an RPC
// stub, never sap's internal URL or the fetch call itself.
const startLoginFn = createServerFn({ method: "POST" })
  .validator((input: { handle: string }) => input)
  .handler(async ({ data }) => {
    try {
      return { redirectUrl: await startLogin(env, data.handle) };
    } catch (err) {
      // Error subclasses don't survive the trip back to the client, so swap
      // in the message the form should show here.
      if (err instanceof EmailDomainNotFoundError) {
        throw new Error(EMAIL_DOMAIN_NOT_FOUND_MESSAGE, { cause: err });
      }
      throw err;
    }
  });

export const Route = createFileRoute("/login")({
  component() {
    return (
      <SignInForm
        onSubmit={async (handle) => {
          const { redirectUrl } = await startLoginFn({ data: { handle } });
          window.location.href = redirectUrl;
          // Keep the button in its loading state while the browser navigates.
          await new Promise(() => {});
        }}
      />
    );
  },
});
