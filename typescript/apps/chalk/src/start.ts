import { createMiddleware, createStart } from "@tanstack/react-start";

// signOutOnUnauthorized runs around every server function: when one fails
// because pear rejected the signed-in member's own OAuth session, it signs
// them out and sends them to /login instead of surfacing the error (see
// signOutIfUnauthorized). The session cookie would otherwise still name a
// member whose every call fails until they sign out by hand.
//
// functions.server is imported inside the server-only body, which the
// build strips from the client bundle, for the reason given at the top of
// that file.
const signOutOnUnauthorized = createMiddleware({ type: "function" }).server(
  async ({ next }) => {
    try {
      return await next();
    } catch (err) {
      const { signOutIfUnauthorized } =
        await import("./server/functions.server");
      await signOutIfUnauthorized(err);
      throw err;
    }
  },
);

export const startInstance = createStart(() => ({
  functionMiddleware: [signOutOnUnauthorized],
}));
